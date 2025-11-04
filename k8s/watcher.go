package k8s

import (
	"context"
	"fmt"
	"sync"
	"time"

	appsv1 "k8s.io/api/apps/v1"
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
// 注意：不再监听 Pod 事件，只通过 Deployment 状态查询 Pod
func (w *Watcher) registerPodHandlers(informer cache.SharedIndexInformer) {
	// 不注册 Pod 事件处理器
	// Pod 的状态变化会反映在 Deployment 的 Ready 状态中
}

// handleDeploymentAdd 处理 Deployment 创建事件
func (w *Watcher) handleDeploymentAdd(obj any) {
	deployment, ok := obj.(*appsv1.Deployment)
	if !ok {
		return
	}

	// 只处理单副本 Deployment
	if deployment.Spec.Replicas == nil || *deployment.Spec.Replicas != 1 {
		return
	}

	// 调用 EventHandler 处理
	// EventHandler 会检查 Deployment 是否就绪，并查询 Pod IP
	if err := w.eventHandler.OnDeploymentAdd(deployment); err != nil {
		// 错误已由 EventHandler 记录
		return
	}
}

// handleDeploymentUpdate 处理 Deployment 更新事件
func (w *Watcher) handleDeploymentUpdate(oldObj, newObj any) {
	oldDeployment, ok1 := oldObj.(*appsv1.Deployment)
	newDeployment, ok2 := newObj.(*appsv1.Deployment)
	if !ok1 || !ok2 {
		return
	}

	// 调用 EventHandler 处理所有更新
	// EventHandler 会根据副本数、就绪状态等决定是创建、更新还是删除路由
	if err := w.eventHandler.OnDeploymentUpdate(oldDeployment, newDeployment); err != nil {
		return
	}
}

// handleDeploymentDelete 处理 Deployment 删除事件
func (w *Watcher) handleDeploymentDelete(obj any) {
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
