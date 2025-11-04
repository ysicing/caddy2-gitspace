package k8s

import (
	"context"
	"fmt"
	"sync"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
)

// EventHandler 处理 Kubernetes 事件的回调接口
type EventHandler interface {
	// OnDeploymentAdd 处理 Deployment 创建事件
	OnDeploymentAdd(deployment *appsv1.Deployment) error

	// OnDeploymentUpdate 处理 Deployment 更新事件
	OnDeploymentUpdate(oldDeployment, newDeployment *appsv1.Deployment) error

	// OnDeploymentDelete 处理 Deployment 删除事件
	OnDeploymentDelete(deployment *appsv1.Deployment) error

	// OnPodUpdate 处理 Pod 更新事件（主要关注就绪状态变化）
	OnPodUpdate(oldPod, newPod *corev1.Pod) error
}

// Watcher 监听 Kubernetes 资源变化
type Watcher struct {
	clientset       kubernetes.Interface
	namespace       string
	informerFactory informers.SharedInformerFactory
	eventHandler    EventHandler
	stopCh          chan struct{}
	ready           bool
	readyMu         sync.RWMutex
}

// NewWatcher 创建新的 Watcher
func NewWatcher(
	clientset kubernetes.Interface,
	namespace string,
	resyncPeriod time.Duration,
	eventHandler EventHandler,
) *Watcher {
	// 创建 SharedInformerFactory（限定命名空间）
	informerFactory := informers.NewSharedInformerFactoryWithOptions(
		clientset,
		resyncPeriod,
		informers.WithNamespace(namespace),
	)

	return &Watcher{
		clientset:       clientset,
		namespace:       namespace,
		informerFactory: informerFactory,
		eventHandler:    eventHandler,
		stopCh:          make(chan struct{}),
		ready:           false,
	}
}

// Start 启动监听器
// 阻塞直到 context 取消或发生致命错误
func (w *Watcher) Start(ctx context.Context) error {
	// 创建 Deployment Informer
	deploymentInformer := w.informerFactory.Apps().V1().Deployments().Informer()

	// 创建 Pod Informer
	podInformer := w.informerFactory.Core().V1().Pods().Informer()

	// 注册事件处理器（将在 T019 中实现）
	w.registerDeploymentHandlers(deploymentInformer)
	w.registerPodHandlers(podInformer)

	// 启动 Informers
	w.informerFactory.Start(w.stopCh)

	// 等待缓存同步
	syncCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	if !cache.WaitForCacheSync(
		syncCtx.Done(),
		deploymentInformer.HasSynced,
		podInformer.HasSynced,
	) {
		return fmt.Errorf("failed to sync informer caches")
	}

	// 标记为就绪
	w.readyMu.Lock()
	w.ready = true
	w.readyMu.Unlock()

	// 阻塞直到停止信号
	select {
	case <-ctx.Done():
		w.Stop()
		return nil
	case <-w.stopCh:
		return nil
	}
}

// Stop 停止监听器
func (w *Watcher) Stop() {
	close(w.stopCh)
	w.readyMu.Lock()
	w.ready = false
	w.readyMu.Unlock()
}

// IsReady 返回监听器是否已准备好（informers已同步）
func (w *Watcher) IsReady() bool {
	w.readyMu.RLock()
	defer w.readyMu.RUnlock()
	return w.ready
}

// registerDeploymentHandlers 注册 Deployment 事件处理器
func (w *Watcher) registerDeploymentHandlers(informer cache.SharedIndexInformer) {
	informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    w.handleDeploymentAdd,
		UpdateFunc: w.handleDeploymentUpdate,
		DeleteFunc: w.handleDeploymentDelete,
	})
}

// registerPodHandlers 注册 Pod 事件处理器
func (w *Watcher) registerPodHandlers(informer cache.SharedIndexInformer) {
	informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		UpdateFunc: w.handlePodUpdate,
	})
}

// handleDeploymentAdd 处理 Deployment 创建事件
func (w *Watcher) handleDeploymentAdd(obj interface{}) {
	deployment, ok := obj.(*appsv1.Deployment)
	if !ok {
		return
	}

	// 只处理单副本 Deployment
	if deployment.Spec.Replicas == nil || *deployment.Spec.Replicas != 1 {
		return
	}

	// 检查是否有就绪的 Pod
	// 如果没有就绪的 Pod，等待 Pod 就绪事件
	if err := w.eventHandler.OnDeploymentAdd(deployment); err != nil {
		// 错误已由 EventHandler 记录
		return
	}
}

// handleDeploymentUpdate 处理 Deployment 更新事件
func (w *Watcher) handleDeploymentUpdate(oldObj, newObj interface{}) {
	oldDeployment, ok1 := oldObj.(*appsv1.Deployment)
	newDeployment, ok2 := newObj.(*appsv1.Deployment)
	if !ok1 || !ok2 {
		return
	}

	// 检查副本数变化
	oldReplicas := int32(0)
	if oldDeployment.Spec.Replicas != nil {
		oldReplicas = *oldDeployment.Spec.Replicas
	}

	newReplicas := int32(0)
	if newDeployment.Spec.Replicas != nil {
		newReplicas = *newDeployment.Spec.Replicas
	}

	// 如果副本数从 1 变为其他值，调用更新处理器
	if oldReplicas != newReplicas {
		if err := w.eventHandler.OnDeploymentUpdate(oldDeployment, newDeployment); err != nil {
			return
		}
	}
}

// handleDeploymentDelete 处理 Deployment 删除事件
func (w *Watcher) handleDeploymentDelete(obj interface{}) {
	deployment, ok := obj.(*appsv1.Deployment)
	if !ok {
		// 处理 DeletedFinalStateUnknown 情况
		tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
		if !ok {
			return
		}
		deployment, ok = tombstone.Obj.(*appsv1.Deployment)
		if !ok {
			return
		}
	}

	if err := w.eventHandler.OnDeploymentDelete(deployment); err != nil {
		return
	}
}

// handlePodUpdate 处理 Pod 更新事件
func (w *Watcher) handlePodUpdate(oldObj, newObj interface{}) {
	oldPod, ok1 := oldObj.(*corev1.Pod)
	newPod, ok2 := newObj.(*corev1.Pod)
	if !ok1 || !ok2 {
		return
	}

	// 检查就绪状态是否变化
	oldReady := IsPodReady(oldPod)
	newReady := IsPodReady(newPod)

	// 只在就绪状态变化时触发事件
	if oldReady != newReady {
		if err := w.eventHandler.OnPodUpdate(oldPod, newPod); err != nil {
			return
		}
	}

	// 检查 Pod IP 变化（Pod 重启场景）
	if oldPod.Status.PodIP != newPod.Status.PodIP && newPod.Status.PodIP != "" {
		if err := w.eventHandler.OnPodUpdate(oldPod, newPod); err != nil {
			return
		}
	}
}
