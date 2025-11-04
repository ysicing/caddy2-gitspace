package router

import (
	"errors"
	"fmt"
	"strings"
)

const (
	// RouteIDPrefixNew 是新的路由 ID 前缀，使用冒号分隔，避免与 Kubernetes 合法字符冲突。
	RouteIDPrefixNew = "k8s:"
)

var ErrInvalidRouteIDFormat = errors.New("invalid route id format")

// BuildRouteID 根据 namespace 和 deployment name 构造稳定的路由 ID。
// 采用冒号分隔，避免与 Kubernetes 允许的 DNS-1123 标签字符冲突。
func BuildRouteID(namespace, name string) string {
	return fmt.Sprintf("%s%s:%s", RouteIDPrefixNew, namespace, name)
}

// ParseRouteID 解析路由 ID，返回 namespace 和 deployment name。
// 支持新的冒号分隔格式以及遗留的连字符格式（k8s-namespace-name）。
func ParseRouteID(routeID string) (string, string, error) {
	if strings.HasPrefix(routeID, RouteIDPrefixNew) {
		rest := strings.TrimPrefix(routeID, RouteIDPrefixNew)
		parts := strings.SplitN(rest, ":", 2)
		if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
			return "", "", fmt.Errorf("%w: %s", ErrInvalidRouteIDFormat, routeID)
		}
		return parts[0], parts[1], nil
	}

	return "", "", fmt.Errorf("%w: %s", ErrInvalidRouteIDFormat, routeID)
}

// IsManagedRouteID 判断给定路由 ID 是否由插件创建。
// 支持新的冒号格式以及遗留的连字符格式。
func IsManagedRouteID(routeID string) bool {
	return strings.HasPrefix(routeID, RouteIDPrefixNew)
}
