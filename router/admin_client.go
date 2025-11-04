package router

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"
)

// AdminAPIClient 封装 Caddy Admin API 调用
type AdminAPIClient struct {
	baseURL    string       // http://localhost:2019
	serverName string       // srv0
	httpClient *http.Client
}

// RouteConfig 路由配置（从 Caddy 返回）
type RouteConfig struct {
	ID         string // @id
	Domain     string // match.host[0]
	TargetAddr string // upstreams[0].dial (格式: "ip:port")
}

// NewAdminAPIClient 创建新的 AdminAPIClient
func NewAdminAPIClient(baseURL, serverName string) *AdminAPIClient {
	return &AdminAPIClient{
		baseURL:    strings.TrimSuffix(baseURL, "/"),
		serverName: serverName,
		httpClient: &http.Client{
			Timeout: 5 * time.Second,
			Transport: &http.Transport{
				MaxIdleConns:        10,
				IdleConnTimeout:     30 * time.Second,
				DisableKeepAlives:   false,
				MaxIdleConnsPerHost: 5,
			},
		},
	}
}

// CreateRoute 通过 Admin API 创建路由
func (c *AdminAPIClient) CreateRoute(
	ctx context.Context,
	routeID, domain, targetIP string,
	targetPort int,
) error {
	// 参数验证
	if routeID == "" {
		return fmt.Errorf("routeID cannot be empty")
	}
	if domain == "" {
		return fmt.Errorf("domain cannot be empty")
	}
	if net.ParseIP(targetIP) == nil {
		return fmt.Errorf("invalid IP address: %s", targetIP)
	}
	if targetPort < 1 || targetPort > 65535 {
		return fmt.Errorf("port out of range (1-65535): %d", targetPort)
	}

	// 构造路由配置
	routeConfig := map[string]any{
		"@id": routeID,
		"match": []map[string]any{
			{
				"host": []string{domain},
			},
		},
		"handle": []map[string]any{
			{
				"handler": "reverse_proxy",
				"upstreams": []map[string]string{
					{
						"dial": fmt.Sprintf("%s:%d", targetIP, targetPort),
					},
				},
			},
		},
	}

	// 序列化为 JSON
	payload, err := json.Marshal(routeConfig)
	if err != nil {
		return fmt.Errorf("failed to marshal route config: %w", err)
	}

	// 发送 POST 请求
	url := fmt.Sprintf("%s/config/apps/http/servers/%s/routes", c.baseURL, c.serverName)
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to call Caddy Admin API: %w", err)
	}
	defer resp.Body.Close()

	// 检查响应状态
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}

	// 读取错误响应
	body, _ := io.ReadAll(resp.Body)
	return fmt.Errorf("Caddy Admin API error: %d - %s", resp.StatusCode, string(body))
}

// DeleteRoute 通过 Admin API 删除路由
// 如果路由不存在（404），不返回错误（幂等）
func (c *AdminAPIClient) DeleteRoute(ctx context.Context, routeID string) error {
	if routeID == "" {
		return fmt.Errorf("routeID cannot be empty")
	}

	// 构造 URL（使用 @id: 前缀）
	url := fmt.Sprintf("%s/config/apps/http/servers/%s/routes/@id:%s",
		c.baseURL, c.serverName, routeID)

	req, err := http.NewRequestWithContext(ctx, "DELETE", url, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to call Caddy Admin API: %w", err)
	}
	defer resp.Body.Close()

	// 200-299: 成功
	// 404: 路由不存在（幂等，不报错）
	if (resp.StatusCode >= 200 && resp.StatusCode < 300) || resp.StatusCode == 404 {
		return nil
	}

	// 其他错误
	body, _ := io.ReadAll(resp.Body)
	return fmt.Errorf("Caddy Admin API error: %d - %s", resp.StatusCode, string(body))
}

// GetRoute 查询路由配置（可选，用于调试）
func (c *AdminAPIClient) GetRoute(ctx context.Context, routeID string) (*RouteConfig, error) {
	if routeID == "" {
		return nil, fmt.Errorf("routeID cannot be empty")
	}

	url := fmt.Sprintf("%s/config/apps/http/servers/%s/routes/@id:%s",
		c.baseURL, c.serverName, routeID)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to call Caddy Admin API: %w", err)
	}
	defer resp.Body.Close()

	// 404: 路由不存在
	if resp.StatusCode == 404 {
		return nil, nil
	}

	// 检查响应状态
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("Caddy Admin API error: %d - %s", resp.StatusCode, string(body))
	}

	// 解析响应
	var rawConfig map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&rawConfig); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	// 提取字段
	config := &RouteConfig{
		ID: routeID,
	}

	// 提取 domain（match.host[0]）
	if match, ok := rawConfig["match"].([]any); ok && len(match) > 0 {
		if matchItem, ok := match[0].(map[string]any); ok {
			if hosts, ok := matchItem["host"].([]any); ok && len(hosts) > 0 {
				config.Domain, _ = hosts[0].(string)
			}
		}
	}

	// 提取 targetAddr（upstreams[0].dial）
	if handle, ok := rawConfig["handle"].([]any); ok && len(handle) > 0 {
		if handleItem, ok := handle[0].(map[string]any); ok {
			if upstreams, ok := handleItem["upstreams"].([]any); ok && len(upstreams) > 0 {
				if upstream, ok := upstreams[0].(map[string]any); ok {
					config.TargetAddr, _ = upstream["dial"].(string)
				}
			}
		}
	}

	return config, nil
}

// ListRoutes 列出所有 k8s-* 路由（用于恢复 RouteIDTracker）
// 返回完整的 RouteConfig 以便缓存 TargetAddr
func (c *AdminAPIClient) ListRoutes(ctx context.Context) ([]*RouteConfig, error) {
	url := fmt.Sprintf("%s/config/apps/http/servers/%s/routes", c.baseURL, c.serverName)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to call Caddy Admin API: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("Caddy Admin API error: %d - %s", resp.StatusCode, string(body))
	}

	// 解析响应（routes 是一个数组）
	var routes []map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&routes); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	// 过滤并解析 k8s-* 路由
	var configs []*RouteConfig
	for _, route := range routes {
		id, ok := route["@id"].(string)
		if !ok || !strings.HasPrefix(id, "k8s-") {
			continue
		}

		config := &RouteConfig{ID: id}

		// 提取 domain（match.host[0]）
		if match, ok := route["match"].([]any); ok && len(match) > 0 {
			if matchItem, ok := match[0].(map[string]any); ok {
				if hosts, ok := matchItem["host"].([]any); ok && len(hosts) > 0 {
					config.Domain, _ = hosts[0].(string)
				}
			}
		}

		// 提取 targetAddr（upstreams[0].dial）
		if handle, ok := route["handle"].([]any); ok && len(handle) > 0 {
			if handleItem, ok := handle[0].(map[string]any); ok {
				if upstreams, ok := handleItem["upstreams"].([]any); ok && len(upstreams) > 0 {
					if upstream, ok := upstreams[0].(map[string]any); ok {
						config.TargetAddr, _ = upstream["dial"].(string)
					}
				}
			}
		}

		configs = append(configs, config)
	}

	return configs, nil
}
