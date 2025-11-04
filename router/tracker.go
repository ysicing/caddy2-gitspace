package router

import (
	"sync"
)

// RouteInfo 路由信息（包含 RouteID 和目标地址）
type RouteInfo struct {
	RouteID    string // Caddy 路由 ID
	TargetAddr string // 目标地址（格式: "ip:port"）
}

// RouteIDTracker 维护 Deployment 到 Route 信息的映射
// 线程安全，缓存 Pod IP 和端口以避免频繁查询 Caddy Admin API
type RouteIDTracker struct {
	// routes 映射: deploymentKey (namespace/name) → RouteInfo
	routes map[string]*RouteInfo
	mu     sync.RWMutex
}

// NewRouteIDTracker 创建新的 RouteIDTracker
func NewRouteIDTracker() *RouteIDTracker {
	return &RouteIDTracker{
		routes: make(map[string]*RouteInfo),
	}
}

// Set 记录 Deployment 到 Route 信息的映射
func (t *RouteIDTracker) Set(deploymentKey, routeID, targetAddr string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.routes[deploymentKey] = &RouteInfo{
		RouteID:    routeID,
		TargetAddr: targetAddr,
	}
}

// Get 查询 Deployment 对应的 Route 信息
// 返回 RouteInfo 和 exists 标志
func (t *RouteIDTracker) Get(deploymentKey string) (*RouteInfo, bool) {
	t.mu.RLock()
	defer t.mu.RUnlock()
	info, exists := t.routes[deploymentKey]
	return info, exists
}

// GetRouteID 仅查询 Route ID（兼容旧代码）
func (t *RouteIDTracker) GetRouteID(deploymentKey string) (string, bool) {
	info, exists := t.Get(deploymentKey)
	if !exists || info == nil {
		return "", false
	}
	return info.RouteID, true
}

// Delete 删除 Deployment 的映射
func (t *RouteIDTracker) Delete(deploymentKey string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.routes, deploymentKey)
}

// List 列出所有映射（用于调试）
// 返回 deploymentKey → RouteInfo 的映射副本
func (t *RouteIDTracker) List() map[string]*RouteInfo {
	t.mu.RLock()
	defer t.mu.RUnlock()

	// 创建副本以避免并发问题
	result := make(map[string]*RouteInfo, len(t.routes))
	for k, v := range t.routes {
		if v != nil {
			result[k] = &RouteInfo{
				RouteID:    v.RouteID,
				TargetAddr: v.TargetAddr,
			}
		}
	}
	return result
}

// Count 返回当前跟踪的路由数量
func (t *RouteIDTracker) Count() int {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return len(t.routes)
}
