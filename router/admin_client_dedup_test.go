package router

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestListRoutesDeduplication 测试 ListRoutes 的去重功能
func TestListRoutesDeduplication(t *testing.T) {
	// 模拟 Caddy Admin API 返回重复路由
	// 使用新的 gitspace: 前缀格式
	duplicatedRoutes := []map[string]any{
		{
			"@id": "gitspace:test-deployment",
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
		},
		{
			"@id": "gitspace:test-deployment", // 重复的路由
			"match": []map[string]any{
				{"host": []string{"test-deployment.example.com"}},
			},
			"handle": []map[string]any{
				{
					"handler": "reverse_proxy",
					"upstreams": []map[string]string{
						{"dial": "10.0.0.1:8080"}, // 相同配置
					},
				},
			},
		},
		{
			"@id": "gitspace:test-deployment", // 再次重复
			"match": []map[string]any{
				{"host": []string{"test-deployment.example.com"}},
			},
			"handle": []map[string]any{
				{
					"handler": "reverse_proxy",
					"upstreams": []map[string]string{
						{"dial": "10.0.0.2:8080"}, // 不同配置
					},
				},
			},
		},
		{
			"@id": "gitspace:another-deployment", // 不同的路由
			"match": []map[string]any{
				{"host": []string{"another-deployment.example.com"}},
			},
			"handle": []map[string]any{
				{
					"handler": "reverse_proxy",
					"upstreams": []map[string]string{
						{"dial": "10.0.0.3:8080"},
					},
				},
			},
		},
	}

	// 创建模拟服务器
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/config/apps/http/servers/srv0/routes" {
			http.NotFound(w, r)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(duplicatedRoutes)
	}))
	defer server.Close()

	// 创建 AdminAPIClient
	client := NewAdminAPIClient(server.URL, "srv0")

	// 调用 ListRoutes
	ctx := context.Background()
	routes, err := client.ListRoutes(ctx)
	if err != nil {
		t.Fatalf("ListRoutes failed: %v", err)
	}

	// 验证去重结果
	// 应该只返回 2 个路由（test-deployment 和 another-deployment）
	if len(routes) != 2 {
		t.Errorf("Expected 2 routes after deduplication, got %d", len(routes))
	}

	// 验证路由 ID
	routeIDs := make(map[string]bool)
	for _, route := range routes {
		routeIDs[route.ID] = true
	}

	expectedIDs := []string{"gitspace:test-deployment", "gitspace:another-deployment"}
	for _, expectedID := range expectedIDs {
		if !routeIDs[expectedID] {
			t.Errorf("Expected route ID %s not found in results", expectedID)
		}
	}

	// 验证重复的路由只保留最后一个配置
	for _, route := range routes {
		if route.ID == "gitspace:test-deployment" {
			// 应该保留最后一个配置（dial: 10.0.0.2:8080）
			if route.TargetAddr != "10.0.0.2:8080" {
				t.Errorf("Expected target address 10.0.0.2:8080, got %s", route.TargetAddr)
			}
		}
	}
}

// TestCleanupDuplicateRoutes 测试清理重复路由功能
func TestCleanupDuplicateRoutes(t *testing.T) {
	deleteRequests := make([]string, 0)

	// 创建模拟服务器
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 处理 GET 请求（列出路由）
		if r.Method == "GET" && r.URL.Path == "/config/apps/http/servers/srv0/routes" {
			// 使用新的 gitspace: 前缀格式
			duplicatedRoutes := []map[string]any{
				{"@id": "gitspace:dup1"},
				{"@id": "gitspace:dup1"}, // 重复
				{"@id": "gitspace:dup1"}, // 重复
				{"@id": "gitspace:unique"},
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(duplicatedRoutes)
			return
		}

		// 处理 DELETE 请求
		if r.Method == "DELETE" {
			deleteRequests = append(deleteRequests, r.URL.Path)
			w.WriteHeader(http.StatusOK)
			return
		}

		http.NotFound(w, r)
	}))
	defer server.Close()

	// 创建 AdminAPIClient
	client := NewAdminAPIClient(server.URL, "srv0")

	// 调用 CleanupDuplicateRoutes
	ctx := context.Background()
	deletedCount, err := client.CleanupDuplicateRoutes(ctx)
	if err != nil {
		t.Fatalf("CleanupDuplicateRoutes failed: %v", err)
	}

	// 验证删除了 3 个重复路由（dup1 出现 3 次）
	if deletedCount != 3 {
		t.Errorf("Expected 3 deleted routes, got %d", deletedCount)
	}

	// 验证发送了正确的 DELETE 请求
	if len(deleteRequests) != 3 {
		t.Errorf("Expected 3 DELETE requests, got %d", len(deleteRequests))
	}
}
