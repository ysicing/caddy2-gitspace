package main

import (
	"context"
	"fmt"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/ysicing/caddy2-k8s/k8s"
	"github.com/ysicing/caddy2-k8s/router"
	"go.uber.org/zap"
)

// EventHandler 实现 k8s.EventHandler 接口
// 连接 Watcher 和 AdminAPIClient
type EventHandler struct {
	adminClient *router.AdminAPIClient
	tracker     *router.RouteIDTracker
	k8sClient   kubernetes.Interface
	namespace   string
	baseDomain  string
	defaultPort int
	logger      *zap.Logger
}

// NewEventHandler 创建新的 EventHandler
func NewEventHandler(
	adminClient *router.AdminAPIClient,
	tracker *router.RouteIDTracker,
	k8sClient kubernetes.Interface,
	namespace string,
	baseDomain string,
	defaultPort int,
	logger *zap.Logger,
) *EventHandler {
	return &EventHandler{
		adminClient: adminClient,
		tracker:     tracker,
		k8sClient:   k8sClient,
		namespace:   namespace,
		baseDomain:  baseDomain,
		defaultPort: defaultPort,
		logger:      logger,
	}
}

// OnDeploymentAdd 处理 Deployment 创建事件
func (h *EventHandler) OnDeploymentAdd(deployment *appsv1.Deployment) error {
	// 只处理单副本 Deployment
	if deployment.Spec.Replicas == nil || *deployment.Spec.Replicas != 1 {
		replicaValue := any("nil")
		if deployment.Spec.Replicas != nil {
			replicaValue = *deployment.Spec.Replicas
		}

		h.logger.Debug("Skipping non-single-replica deployment",
			zap.String("deployment", deployment.Name),
			zap.Any("replicas", replicaValue),
		)
		return nil
	}

	// 检查 Deployment 是否就绪
	if !isDeploymentReady(deployment) {
		h.logger.Debug("Deployment not ready yet, skipping",
			zap.String("deployment", deployment.Name),
		)
		return nil
	}

	// 查找就绪的 Pod
	pod, err := h.findReadyPod(deployment)
	if err != nil {
		h.logger.Error("Failed to find ready pod",
			zap.String("deployment", deployment.Name),
			zap.Error(err),
		)
		return err
	}

	if pod == nil {
		h.logger.Debug("No ready pod found",
			zap.String("deployment", deployment.Name),
		)
		return nil
	}

	// 创建路由
	return h.createRoute(deployment, pod)
}

// OnDeploymentUpdate 处理 Deployment 更新事件
func (h *EventHandler) OnDeploymentUpdate(oldDeployment, newDeployment *appsv1.Deployment) error {
	oldReplicas := int32(0)
	if oldDeployment.Spec.Replicas != nil {
		oldReplicas = *oldDeployment.Spec.Replicas
	}

	newReplicas := int32(0)
	if newDeployment.Spec.Replicas != nil {
		newReplicas = *newDeployment.Spec.Replicas
	}

	oldReady := isDeploymentReady(oldDeployment)
	newReady := isDeploymentReady(newDeployment)

	// 场景 1: 副本数从 1 变为其他值 → 删除路由
	if oldReplicas == 1 && newReplicas != 1 {
		h.logger.Info("Deployment replicas changed from 1, deleting route",
			zap.String("deployment", newDeployment.Name),
			zap.Int32("new_replicas", newReplicas),
		)
		return h.deleteRoute(newDeployment)
	}

	// 场景 2: 副本数从其他值变为 1 → 尝试创建路由
	if oldReplicas != 1 && newReplicas == 1 {
		h.logger.Info("Deployment replicas changed to 1",
			zap.String("deployment", newDeployment.Name),
		)
		return h.OnDeploymentAdd(newDeployment)
	}

	// 场景 3: 副本数保持为 1，但就绪状态变化
	if newReplicas == 1 {
		// 从未就绪变为就绪 → 创建路由
		if !oldReady && newReady {
			h.logger.Info("Deployment became ready, creating route",
				zap.String("deployment", newDeployment.Name),
			)
			return h.OnDeploymentAdd(newDeployment)
		}

		// 从就绪变为未就绪 → 删除路由
		if oldReady && !newReady {
			h.logger.Info("Deployment became not ready, deleting route",
				zap.String("deployment", newDeployment.Name),
			)
			return h.deleteRoute(newDeployment)
		}

		// 保持就绪状态 → 可能是 Pod 重建（IP 变化）
		// 使用缓存的 TargetAddr 检查 Pod IP 是否变化，避免频繁调用 GetRoute
		if oldReady && newReady {
			// 检查是否有就绪的 Pod
			pod, err := h.findReadyPod(newDeployment)
			if err != nil {
				return err
			}

			if pod != nil {
				// 计算期望的 target address
				expectedAddr := fmt.Sprintf("%s:%d", pod.Status.PodIP, getPortFromDeployment(newDeployment, h.defaultPort))

				// 从 Tracker 查询缓存的路由信息
				deploymentKey := fmt.Sprintf("%s/%s", newDeployment.Namespace, newDeployment.Name)
				routeInfo, exists := h.tracker.Get(deploymentKey)

				if exists && routeInfo != nil {
					// 比较缓存的 TargetAddr 与期望值
					if routeInfo.TargetAddr != expectedAddr {
						h.logger.Info("Pod IP changed, updating route",
							zap.String("deployment", newDeployment.Name),
							zap.String("old_target", routeInfo.TargetAddr),
							zap.String("new_target", expectedAddr),
						)
						// 删除旧路由
						if err := h.deleteRoute(newDeployment); err != nil {
							h.logger.Error("Failed to delete old route", zap.Error(err))
						}
						// 创建新路由
						return h.createRoute(newDeployment, pod)
					}
					// Pod IP 没有变化，跳过更新
				} else {
					// 没有路由，创建新路由
					return h.createRoute(newDeployment, pod)
				}
			}
		}
	}

	return nil
}

// OnDeploymentDelete 处理 Deployment 删除事件
func (h *EventHandler) OnDeploymentDelete(deployment *appsv1.Deployment) error {
	return h.deleteRoute(deployment)
}

// createRoute 创建路由
func (h *EventHandler) createRoute(deployment *appsv1.Deployment, pod *corev1.Pod) error {
	// 读取端口注解
	port, err := k8s.GetPortFromAnnotation(deployment.Annotations, h.defaultPort)
	if err != nil {
		h.logger.Warn("Invalid port annotation, using default",
			zap.String("deployment", deployment.Name),
			zap.Int("default_port", h.defaultPort),
			zap.Error(err),
		)
		port = h.defaultPort
	}

	// 生成 Route ID 和域名
	deploymentKey := fmt.Sprintf("%s/%s", deployment.Namespace, deployment.Name)
	routeID := router.BuildRouteID(deployment.Namespace, deployment.Name)
	domain := fmt.Sprintf("%s.%s", deployment.Name, h.baseDomain)

	// 调用 Admin API 创建路由
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := h.adminClient.CreateRoute(ctx, routeID, domain, pod.Status.PodIP, port); err != nil {
		h.logger.Error("Failed to create route",
			zap.String("deployment", deployment.Name),
			zap.String("route_id", routeID),
			zap.String("domain", domain),
			zap.Error(err),
		)
		return err
	}

	// 记录到 Tracker（缓存 RouteID 和 TargetAddr）
	targetAddr := fmt.Sprintf("%s:%d", pod.Status.PodIP, port)
	h.tracker.Set(deploymentKey, routeID, targetAddr)

	h.logger.Info("Route created",
		zap.String("deployment", deployment.Name),
		zap.String("domain", domain),
		zap.String("target", fmt.Sprintf("%s:%d", pod.Status.PodIP, port)),
	)

	// 写回注解到 Deployment
	annotations := map[string]string{
		k8s.AnnotationURL:     domain,
		k8s.AnnotationSynced:  time.Now().Format(time.RFC3339),
		k8s.AnnotationRouteID: routeID,
	}

	ctx2, cancel2 := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel2()

	if err := k8s.PatchDeploymentAnnotation(ctx2, h.k8sClient, deployment.Namespace, deployment.Name, annotations); err != nil {
		h.logger.Warn("Failed to patch deployment annotations",
			zap.String("deployment", deployment.Name),
			zap.Error(err),
		)
		// 不返回错误，因为路由已经创建成功
	}

	return nil
}

// deleteRoute 删除路由
func (h *EventHandler) deleteRoute(deployment *appsv1.Deployment) error {
	deploymentKey := fmt.Sprintf("%s/%s", deployment.Namespace, deployment.Name)

	// 从 Tracker 查找 Route 信息
	routeInfo, exists := h.tracker.Get(deploymentKey)
	if !exists || routeInfo == nil {
		h.logger.Debug("No route to delete",
			zap.String("deployment", deployment.Name),
		)
		return nil
	}

	// 调用 Admin API 删除路由
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := h.adminClient.DeleteRoute(ctx, routeInfo.RouteID); err != nil {
		h.logger.Error("Failed to delete route",
			zap.String("deployment", deployment.Name),
			zap.String("route_id", routeInfo.RouteID),
			zap.Error(err),
		)
		return err
	}

	// 清理 Tracker
	h.tracker.Delete(deploymentKey)

	h.logger.Info("Route deleted",
		zap.String("deployment", deployment.Name),
		zap.String("route_id", routeInfo.RouteID),
	)

	return nil
}

// findReadyPod 查找 Deployment 的就绪 Pod
func (h *EventHandler) findReadyPod(deployment *appsv1.Deployment) (*corev1.Pod, error) {
	// 使用 label selector 查找 Pod
	labelSelector := metav1.FormatLabelSelector(deployment.Spec.Selector)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	pods, err := h.k8sClient.CoreV1().Pods(deployment.Namespace).List(ctx, metav1.ListOptions{
		LabelSelector: labelSelector,
	})
	if err != nil {
		return nil, err
	}

	// 查找第一个就绪的 Pod
	for i := range pods.Items {
		if k8s.IsPodReady(&pods.Items[i]) {
			return &pods.Items[i], nil
		}
	}

	return nil, nil
}

// isDeploymentReady 检查 Deployment 是否就绪
func isDeploymentReady(deployment *appsv1.Deployment) bool {
	// 检查 Deployment 的状态条件
	for _, cond := range deployment.Status.Conditions {
		if cond.Type == appsv1.DeploymentAvailable {
			return cond.Status == corev1.ConditionTrue
		}
	}
	return false
}

// getPortFromDeployment 从 Deployment 获取端口
func getPortFromDeployment(deployment *appsv1.Deployment, defaultPort int) int {
	port, err := k8s.GetPortFromAnnotation(deployment.Annotations, defaultPort)
	if err != nil {
		return defaultPort
	}
	return port
}

// Interface guard
var _ k8s.EventHandler = (*EventHandler)(nil)
