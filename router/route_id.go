package router

import (
	"errors"
	"strings"
)

var ErrInvalidRouteIDFormat = errors.New("invalid route id format")

// BuildRouteID 根据 gitspace identifier 构造路由 ID。
// 由于 Caddy 实例专用于 gitspace，直接使用 identifier，无需前缀。
func BuildRouteID(gitspaceIdentifier string) string {
	return gitspaceIdentifier
}

// ParseRouteID 解析路由 ID，返回 gitspace identifier。
// 由于没有前缀，直接返回 routeID 本身。
func ParseRouteID(routeID string) (string, error) {
	if routeID == "" {
		return "", ErrInvalidRouteIDFormat
	}
	return routeID, nil
}

// IsManagedRouteID 判断给定路由 ID 是否由插件创建。
// 由于 Caddy 实例专用于 gitspace，所有非空且不包含路径分隔符的 ID 都认为是插件管理的。
func IsManagedRouteID(routeID string) bool {
	if routeID == "" {
		return false
	}

	// 排除明显不是我们管理的路由（如包含特殊路径字符）
	if strings.Contains(routeID, "/") || strings.Contains(routeID, "\\") {
		return false
	}

	return true
}
