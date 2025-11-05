package router

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestCreateRouteIdempotent 测试 CreateRoute 的幂等性
func TestCreateRouteIdempotent(t *testing.T) {
	getCallCount := 0
	postCallCount := 0
	deleteCallCount := 0

	// 模拟服务器
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// GET 请求 - 查询路由（使用新的 /id/ 端点）
		if r.Method == "GET" && strings.HasPrefix(r.URL.Path, "/id/") {
			getCallCount++

			// 第一次调用返回 404（路由不存在）
			if getCallCount == 1 {
				w.WriteHeader(http.StatusNotFound)
				return
			}

			// 第二次调用返回已存在的路由（配置一致）
			existingRoute := map[string]any{
				"@id": "test-deployment",
				"match": []map[string]any{
					{"host": []string{"test-deployment.example.com"}},
				},
				"handle": []map[string]any{
					{
						"handler": "reverse_proxy",
						"upstreams": []map[string]string{
							{"dial": "10.0.0.1:8080"},
						},
					},
				},
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(existingRoute)
			return
		}

		// POST 请求 - 创建路由
		if r.Method == "POST" && strings.HasSuffix(r.URL.Path, "/routes") {
			postCallCount++
			w.WriteHeader(http.StatusOK)
			return
		}

		// DELETE 请求
		if r.Method == "DELETE" {
			deleteCallCount++
			w.WriteHeader(http.StatusOK)
			return
		}

		http.NotFound(w, r)
	}))
	defer server.Close()

	client := NewAdminAPIClient(server.URL, "srv0")
	ctx := context.Background()

	// 第一次调用 - 路由不存在，应该创建
	err := client.CreateRoute(ctx, "test-deployment", "test-deployment.example.com", "10.0.0.1", 8080)
	if err != nil {
		t.Fatalf("First CreateRoute failed: %v", err)
	}

	if getCallCount != 1 {
		t.Errorf("Expected 1 GET call, got %d", getCallCount)
	}
	if postCallCount != 1 {
		t.Errorf("Expected 1 POST call, got %d", postCallCount)
	}

	// 第二次调用 - 路由已存在且配置一致，应该跳过创建（幂等）
	err = client.CreateRoute(ctx, "test-deployment", "test-deployment.example.com", "10.0.0.1", 8080)
	if err != nil {
		t.Fatalf("Second CreateRoute failed: %v", err)
	}

	if getCallCount != 2 {
		t.Errorf("Expected 2 GET calls, got %d", getCallCount)
	}
	if postCallCount != 1 {
		t.Errorf("Expected still 1 POST call (idempotent), got %d", postCallCount)
	}
}

// TestCreateRouteUpdateWhenChanged 测试配置变化时更新路由
func TestCreateRouteUpdateWhenChanged(t *testing.T) {
	deleteCallCount := 0
	postCallCount := 0

	// 模拟服务器
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// GET 请求 - 返回旧配置的路由（使用新的 /id/ 端点）
		if r.Method == "GET" && strings.HasPrefix(r.URL.Path, "/id/") {
			existingRoute := map[string]any{
				"@id": "test-deployment",
				"match": []map[string]any{
					{"host": []string{"test-deployment.example.com"}},
				},
				"handle": []map[string]any{
					{
						"handler": "reverse_proxy",
						"upstreams": []map[string]string{
							{"dial": "10.0.0.1:8080"}, // 旧 IP
						},
					},
				},
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(existingRoute)
			return
		}

		// POST 请求 - 创建路由
		if r.Method == "POST" && strings.HasSuffix(r.URL.Path, "/routes") {
			postCallCount++
			w.WriteHeader(http.StatusOK)
			return
		}

		// DELETE 请求
		if r.Method == "DELETE" {
			deleteCallCount++
			w.WriteHeader(http.StatusOK)
			return
		}

		http.NotFound(w, r)
	}))
	defer server.Close()

	client := NewAdminAPIClient(server.URL, "srv0")
	ctx := context.Background()

	// 调用 CreateRoute，但 IP 已变化
	err := client.CreateRoute(ctx, "test-deployment", "test-deployment.example.com", "10.0.0.2", 8080)
	if err != nil {
		t.Fatalf("CreateRoute failed: %v", err)
	}

	// 应该先删除旧路由，再创建新路由
	if deleteCallCount != 1 {
		t.Errorf("Expected 1 DELETE call, got %d", deleteCallCount)
	}
	if postCallCount != 1 {
		t.Errorf("Expected 1 POST call, got %d", postCallCount)
	}
}

// TestCreateRoutePreventsMultiplePOST 测试多次调用不会重复添加路由
func TestCreateRoutePreventsMultiplePOST(t *testing.T) {
	postCallCount := 0

	// 模拟服务器 - 始终返回路由已存在
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// GET 请求 - 始终返回路由已存在（使用新的 /id/ 端点）
		if r.Method == "GET" && strings.HasPrefix(r.URL.Path, "/id/") {
			existingRoute := map[string]any{
				"@id": "test-deployment",
				"match": []map[string]any{
					{"host": []string{"test-deployment.example.com"}},
				},
				"handle": []map[string]any{
					{
						"handler": "reverse_proxy",
						"upstreams": []map[string]string{
							{"dial": "10.0.0.1:8080"},
						},
					},
				},
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(existingRoute)
			return
		}

		// POST 请求
		if r.Method == "POST" && strings.HasSuffix(r.URL.Path, "/routes") {
			postCallCount++
			w.WriteHeader(http.StatusOK)
			return
		}

		http.NotFound(w, r)
	}))
	defer server.Close()

	client := NewAdminAPIClient(server.URL, "srv0")
	ctx := context.Background()

	// 连续调用 10 次
	for i := range 10 {
		err := client.CreateRoute(ctx, "test-deployment", "test-deployment.example.com", "10.0.0.1", 8080)
		if err != nil {
			t.Fatalf("CreateRoute call %d failed: %v", i+1, err)
		}
	}

	// 由于路由始终存在且配置一致，不应该有任何 POST 调用
	if postCallCount != 0 {
		t.Errorf("Expected 0 POST calls (all idempotent), got %d", postCallCount)
	}
}
