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
		h.logger.Debug("Skipping non-single-replica deployment",
			zap.String("deployment", deployment.Name),
			zap.Int32("replicas", *deployment.Spec.Replicas),
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
		// 没有就绪的 Pod，等待 Pod 就绪事件
		h.logger.Debug("No ready pod found, waiting",
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

	// 如果副本数从 1 变为 0 或其他值，删除路由
	if oldReplicas == 1 && newReplicas != 1 {
		h.logger.Info("Deployment replicas changed from 1, deleting route",
			zap.String("deployment", newDeployment.Name),
			zap.Int32("old_replicas", oldReplicas),
			zap.Int32("new_replicas", newReplicas),
		)
		return h.deleteRoute(newDeployment)
	}

	// 如果副本数从其他值变为 1，创建路由
	if oldReplicas != 1 && newReplicas == 1 {
		h.logger.Info("Deployment replicas changed to 1, creating route",
			zap.String("deployment", newDeployment.Name),
		)
		return h.OnDeploymentAdd(newDeployment)
	}

	return nil
}

// OnDeploymentDelete 处理 Deployment 删除事件
func (h *EventHandler) OnDeploymentDelete(deployment *appsv1.Deployment) error {
	return h.deleteRoute(deployment)
}

// OnPodUpdate 处理 Pod 更新事件
func (h *EventHandler) OnPodUpdate(oldPod, newPod *corev1.Pod) error {
	// 获取 Pod 所属的 Deployment
	deployment, err := h.getDeploymentFromPod(newPod)
	if err != nil || deployment == nil {
		return err
	}

	// 只处理单副本 Deployment
	if deployment.Spec.Replicas == nil || *deployment.Spec.Replicas != 1 {
		return nil
	}

	// 检查就绪状态变化
	oldReady := k8s.IsPodReady(oldPod)
	newReady := k8s.IsPodReady(newPod)

	if !oldReady && newReady {
		// Pod 从未就绪变为就绪，创建路由
		h.logger.Info("Pod became ready, creating route",
			zap.String("deployment", deployment.Name),
			zap.String("pod", newPod.Name),
			zap.String("pod_ip", newPod.Status.PodIP),
		)
		return h.createRoute(deployment, newPod)
	}

	if oldReady && !newReady {
		// Pod 从就绪变为未就绪，删除路由
		h.logger.Info("Pod became not ready, deleting route",
			zap.String("deployment", deployment.Name),
			zap.String("pod", newPod.Name),
		)
		return h.deleteRoute(deployment)
	}

	// 检查 Pod IP 变化（Pod 重启）
	if oldPod.Status.PodIP != newPod.Status.PodIP && newPod.Status.PodIP != "" && newReady {
		h.logger.Info("Pod IP changed, updating route",
			zap.String("deployment", deployment.Name),
			zap.String("old_ip", oldPod.Status.PodIP),
			zap.String("new_ip", newPod.Status.PodIP),
		)
		// 删除旧路由 + 创建新路由
		if err := h.deleteRoute(deployment); err != nil {
			h.logger.Error("Failed to delete old route", zap.Error(err))
		}
		return h.createRoute(deployment, newPod)
	}

	return nil
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
	routeID := fmt.Sprintf("k8s-%s-%s", deployment.Namespace, deployment.Name)
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

	// 记录到 Tracker
	h.tracker.Set(deploymentKey, routeID)

	h.logger.Info("Route created",
		zap.String("deployment", deployment.Name),
		zap.String("domain", domain),
		zap.String("target", fmt.Sprintf("%s:%d", pod.Status.PodIP, port)),
	)

	// 写回注解到 Deployment
	annotations := map[string]string{
		k8s.AnnotationURL:      domain,
		k8s.AnnotationSynced:   time.Now().Format(time.RFC3339),
		k8s.AnnotationRouteID:  routeID,
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

	// 从 Tracker 查找 Route ID
	routeID, exists := h.tracker.Get(deploymentKey)
	if !exists {
		h.logger.Debug("No route to delete",
			zap.String("deployment", deployment.Name),
		)
		return nil
	}

	// 调用 Admin API 删除路由
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := h.adminClient.DeleteRoute(ctx, routeID); err != nil {
		h.logger.Error("Failed to delete route",
			zap.String("deployment", deployment.Name),
			zap.String("route_id", routeID),
			zap.Error(err),
		)
		return err
	}

	// 清理 Tracker
	h.tracker.Delete(deploymentKey)

	h.logger.Info("Route deleted",
		zap.String("deployment", deployment.Name),
		zap.String("route_id", routeID),
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

// getDeploymentFromPod 从 Pod 获取所属的 Deployment
func (h *EventHandler) getDeploymentFromPod(pod *corev1.Pod) (*appsv1.Deployment, error) {
	// 从 OwnerReferences 查找 Deployment
	for _, owner := range pod.OwnerReferences {
		if owner.Kind == "ReplicaSet" {
			// 查找 ReplicaSet
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			rs, err := h.k8sClient.AppsV1().ReplicaSets(pod.Namespace).Get(ctx, owner.Name, metav1.GetOptions{})
			if err != nil {
				return nil, err
			}

			// 从 ReplicaSet 查找 Deployment
			for _, rsOwner := range rs.OwnerReferences {
				if rsOwner.Kind == "Deployment" {
					ctx2, cancel2 := context.WithTimeout(context.Background(), 5*time.Second)
					defer cancel2()

					deployment, err := h.k8sClient.AppsV1().Deployments(pod.Namespace).Get(ctx2, rsOwner.Name, metav1.GetOptions{})
					if err != nil {
						return nil, err
					}
					return deployment, nil
				}
			}
		}
	}

	return nil, nil
}

// Interface guard
var _ k8s.EventHandler = (*EventHandler)(nil)
