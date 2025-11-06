package k8s

import (
	"fmt"
	"strconv"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
)

// 注解常量
const (
	// AnnotationPort Deployment 上指定目标端口的注解键
	AnnotationPort = "gitspace.caddy.default.port"

	// AnnotationURL 路由创建成功后写回的域名注解键
	AnnotationURL = "gitspace.caddy.route.url"

	// AnnotationSynced 路由同步时间戳注解键
	AnnotationSynced = "gitspace.caddy.route.synced-at"

	// AnnotationRouteID 路由 ID 注解键
	AnnotationRouteID = "gitspace.caddy.route.id"
)

// isPodReady 检查 Pod 是否就绪
func IsPodReady(pod *corev1.Pod) bool {
	for _, cond := range pod.Status.Conditions {
		if cond.Type == corev1.PodReady {
			return cond.Status == corev1.ConditionTrue
		}
	}
	return false
}

// getPortFromAnnotation 从 Deployment 注解中读取端口
// 如果注解不存在或无效，返回 defaultPort
func GetPortFromAnnotation(annotations map[string]string, defaultPort int) (int, error) {
	portStr, exists := annotations[AnnotationPort]
	if !exists {
		return defaultPort, nil
	}

	port, err := strconv.Atoi(portStr)
	if err != nil {
		return 0, fmt.Errorf("invalid port annotation '%s': %w", portStr, err)
	}

	if port < 1 || port > 65535 {
		return 0, fmt.Errorf("port out of range (1-65535): %d", port)
	}

	return port, nil
}

// DesiredReplicaCount 返回 Deployment 期望的副本数量。
// 按 Kubernetes 语义，当 spec.replicas 为空时默认值为 1。
func DesiredReplicaCount(deployment *appsv1.Deployment) int32 {
	if deployment == nil || deployment.Spec.Replicas == nil {
		return 1
	}
	return *deployment.Spec.Replicas
}

// GetGitspaceIdentifier 从 Deployment labels 中提取 gitspace identifier
// 这是稳定的配置级别标识符，不同于可能包含实例后缀的 deployment name
// 如果 label 不存在，返回空字符串
func GetGitspaceIdentifier(deployment *appsv1.Deployment) string {
	if deployment == nil {
		return ""
	}

	// 从 label 中获取 gitspace identifier
	if identifier, exists := deployment.Labels["gitspace"]; exists && identifier != "" {
		return identifier
	}

	// label 不存在，返回空字符串
	return ""
}
