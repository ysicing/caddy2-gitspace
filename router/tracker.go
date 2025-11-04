package router

import (
	"sync"
)

// RouteIDTracker 维护 Deployment 到 Route ID 的映射
// 线程安全
type RouteIDTracker struct {
	// routes 映射: deploymentKey (namespace/name) → routeID
	routes map[string]string
	mu     sync.RWMutex
}

// NewRouteIDTracker 创建新的 RouteIDTracker
func NewRouteIDTracker() *RouteIDTracker {
	return &RouteIDTracker{
		routes: make(map[string]string),
	}
}

// Set 记录 Deployment 到 Route ID 的映射
func (t *RouteIDTracker) Set(deploymentKey, routeID string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.routes[deploymentKey] = routeID
}

// Get 查询 Deployment 对应的 Route ID
// 返回 routeID 和 exists 标志
func (t *RouteIDTracker) Get(deploymentKey string) (string, bool) {
	t.mu.RLock()
	defer t.mu.RUnlock()
	routeID, exists := t.routes[deploymentKey]
	return routeID, exists
}

// Delete 删除 Deployment 的映射
func (t *RouteIDTracker) Delete(deploymentKey string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.routes, deploymentKey)
}

// List 列出所有映射（用于调试）
// 返回 deploymentKey → routeID 的映射副本
func (t *RouteIDTracker) List() map[string]string {
	t.mu.RLock()
	defer t.mu.RUnlock()

	// 创建副本以避免并发问题
	result := make(map[string]string, len(t.routes))
	for k, v := range t.routes {
		result[k] = v
	}
	return result
}

// Count 返回当前跟踪的路由数量
func (t *RouteIDTracker) Count() int {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return len(t.routes)
}
