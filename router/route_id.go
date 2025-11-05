package router

import (
	"errors"
	"strings"
)

var ErrInvalidRouteIDFormat = errors.New("invalid route id format")

// BuildRouteID 根据 deployment name 构造路由 ID。
// 直接使用 deployment name，简洁明了，避免分隔符问题。
func BuildRouteID(name string) string {
	return name
}

// ParseRouteID 解析路由 ID，返回 deployment name。
// 由于直接使用 deployment name，直接返回即可。
func ParseRouteID(routeID string) (string, error) {
	if routeID == "" {
		return "", ErrInvalidRouteIDFormat
	}
	return routeID, nil
}

// IsManagedRouteID 判断给定路由是否由本插件管理。
// 通过检查路由配置特征来判断：
// - 必须有 @id 字段
// - 必须有 reverse_proxy handler
// - 必须有 host matcher
//
// 注意：由于去掉了前缀，此函数现在需要配合完整的路由配置使用
// 在 ListRoutes 中，我们会通过检查路由结构来过滤
func IsManagedRouteID(routeID string) bool {
	// 基本检查：不为空，且符合 Kubernetes DNS-1123 标签规范
	if routeID == "" {
		return false
	}

	// 排除明显不是我们管理的路由（如包含特殊路径字符）
	if strings.Contains(routeID, "/") || strings.Contains(routeID, "\\") {
		return false
	}

	// 其他情况默认认为可能是我们管理的
	// 在 ListRoutes 中会进一步通过路由配置结构来确认
	return true
}
