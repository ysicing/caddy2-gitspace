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
	baseURL    string // http://localhost:2019
	serverName string // srv0
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
			// 移除全局超时,使用 context 控制每个请求的超时
			// Timeout: 5 * time.Second,
			Transport: &http.Transport{
				MaxIdleConns:        10,
				IdleConnTimeout:     30 * time.Second,
				DisableKeepAlives:   false,
				MaxIdleConnsPerHost: 5,
				// 添加连接超时,避免连接建立阶段阻塞
				DialContext: (&net.Dialer{
					Timeout:   2 * time.Second,
					KeepAlive: 30 * time.Second,
				}).DialContext,
				// 添加 TLS 握手超时
				TLSHandshakeTimeout: 2 * time.Second,
				// 添加响应头超时
				ResponseHeaderTimeout: 5 * time.Second,
			},
		},
	}
}

// CreateRoute 通过 Admin API 创建路由（幂等操作）
// 会先检查路由是否已存在，如果存在且配置一致则跳过创建
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

	expectedTargetAddr := fmt.Sprintf("%s:%d", targetIP, targetPort)

	// 幂等性检查: 先查询路由是否已存在
	existingRoute, err := c.GetRoute(ctx, routeID)
	if err != nil {
		return fmt.Errorf("failed to check existing route: %w", err)
	}

	if existingRoute != nil {
		// 路由已存在，检查配置是否一致
		if existingRoute.Domain == domain && existingRoute.TargetAddr == expectedTargetAddr {
			// 配置完全一致，跳过创建（幂等）
			return nil
		}

		// 配置不一致，先删除旧路由
		if err := c.DeleteRoute(ctx, routeID); err != nil {
			return fmt.Errorf("failed to delete old route before recreating: %w", err)
		}
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
						"dial": expectedTargetAddr,
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
// 使用 /id/{routeID} 端点直接删除配置
func (c *AdminAPIClient) DeleteRoute(ctx context.Context, routeID string) error {
	if routeID == "" {
		return fmt.Errorf("routeID cannot be empty")
	}

	// 使用 /id/ 端点删除配置
	url := fmt.Sprintf("%s/id/%s", c.baseURL, routeID)

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
// 使用 /id/{routeID} 端点直接访问配置
func (c *AdminAPIClient) GetRoute(ctx context.Context, routeID string) (*RouteConfig, error) {
	if routeID == "" {
		return nil, fmt.Errorf("routeID cannot be empty")
	}

	// 使用 /id/ 端点访问配置
	url := fmt.Sprintf("%s/id/%s", c.baseURL, routeID)

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

// ListRoutes 列出所有由插件管理的路由（用于恢复 RouteIDTracker）
// 返回完整的 RouteConfig 以便缓存 TargetAddr
// 自动去重: 如果存在多个相同 @id 的路由,只保留最后一个
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

	// 使用 map 去重: routeID -> RouteConfig
	// 如果存在重复的 @id,后面的会覆盖前面的
	configMap := make(map[string]*RouteConfig)

	for _, route := range routes {
		id, ok := route["@id"].(string)
		if !ok || !IsManagedRouteID(id) {
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

		// 去重: 如果已存在相同 ID,覆盖之前的配置
		configMap[id] = config
	}

	// 将 map 转换为数组
	configs := make([]*RouteConfig, 0, len(configMap))
	for _, config := range configMap {
		configs = append(configs, config)
	}

	return configs, nil
}

// CleanupDuplicateRoutes 清理 Caddy 配置中的重复路由
// 返回删除的重复路由数量和遇到的所有错误
func (c *AdminAPIClient) CleanupDuplicateRoutes(ctx context.Context) (int, error) {
	url := fmt.Sprintf("%s/config/apps/http/servers/%s/routes", c.baseURL, c.serverName)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return 0, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return 0, fmt.Errorf("failed to call Caddy Admin API: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return 0, fmt.Errorf("Caddy Admin API error: %d - %s", resp.StatusCode, string(body))
	}

	// 解析响应（routes 是一个数组）
	var routes []map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&routes); err != nil {
		return 0, fmt.Errorf("failed to parse response: %w", err)
	}

	// 识别重复的路由: routeID -> 出现次数
	seenIDs := make(map[string]int)
	managedRoutes := make([]string, 0)

	for _, route := range routes {
		id, ok := route["@id"].(string)
		if !ok || !IsManagedRouteID(id) {
			continue
		}
		managedRoutes = append(managedRoutes, id)
		seenIDs[id]++
	}

	// 查找需要删除的重复路由
	duplicateCount := 0
	for id, count := range seenIDs {
		if count > 1 {
			// 删除所有该 ID 的路由(因为无法区分哪个是"正确"的)
			// 之后会通过对账或事件重建正确的路由
			for range count {
				if err := c.DeleteRoute(ctx, id); err != nil {
					// 记录错误但继续清理其他重复项
					return duplicateCount, fmt.Errorf("failed to delete duplicate route %s: %w", id, err)
				}
				duplicateCount++
			}
		}
	}

	return duplicateCount, nil
}

// HealthCheck 检查 Caddy Admin API 是否可访问
func (c *AdminAPIClient) HealthCheck(ctx context.Context, endpoint string) error {
	url := fmt.Sprintf("%s%s", c.baseURL, endpoint)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("Admin API unreachable: %w", err)
	}
	defer resp.Body.Close()

	// 只要能返回响应就认为健康(即使是 404)
	if resp.StatusCode == 404 || (resp.StatusCode >= 200 && resp.StatusCode < 500) {
		return nil
	}

	body, _ := io.ReadAll(resp.Body)
	return fmt.Errorf("Admin API returned error: %d - %s", resp.StatusCode, string(body))
}
