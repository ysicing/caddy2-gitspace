package router

import (
	"errors"
	"fmt"
	"strings"
)

const (
	// RouteIDPrefixNew 是新的路由 ID 前缀，使用冒号分隔，避免与 Kubernetes 合法字符冲突。
	RouteIDPrefixNew = "gitspace:"
)

var ErrInvalidRouteIDFormat = errors.New("invalid route id format")

// BuildRouteID 根据 gitspace identifier 构造稳定的路由 ID。
// 采用冒号分隔，避免与 Kubernetes 允许的 DNS-1123 标签字符冲突。
func BuildRouteID(gitspaceIdentifier string) string {
	return fmt.Sprintf("%s:%s", RouteIDPrefixNew, gitspaceIdentifier)
}

// ParseRouteID 解析路由 ID，返回 gitspace identifier。
// namespace 已固定在配置中，不需要从 RouteID 解析。
func ParseRouteID(routeID string) (string, error) {
	if strings.HasPrefix(routeID, RouteIDPrefixNew) {
		gitspaceIdentifier := strings.TrimPrefix(routeID, RouteIDPrefixNew)
		if gitspaceIdentifier == "" {
			return "", fmt.Errorf("%w: %s", ErrInvalidRouteIDFormat, routeID)
		}
		return gitspaceIdentifier, nil
	}

	return "", fmt.Errorf("%w: %s", ErrInvalidRouteIDFormat, routeID)
}

// IsManagedRouteID 判断给定路由 ID 是否由插件创建。
// 支持新的冒号格式以及遗留的连字符格式。
func IsManagedRouteID(routeID string) bool {
	return strings.HasPrefix(routeID, RouteIDPrefixNew)
}
