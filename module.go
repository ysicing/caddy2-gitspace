package caddy2k8s

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/caddy/v2/caddyconfig/caddyfile"
	"github.com/ysicing/caddy2-gitspace/config"
	"github.com/ysicing/caddy2-gitspace/k8s"
	"github.com/ysicing/caddy2-gitspace/router"
	"go.uber.org/zap"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

func init() {
	caddy.RegisterModule(K8sRouter{})
}

// K8sRouter 实现 Caddy 模块接口
type K8sRouter struct {
	// 配置字段（从 Caddyfile/JSON 加载）
	Namespace       string `json:"namespace"`
	BaseDomain      string `json:"base_domain"`
	DefaultPort     int    `json:"default_port,omitempty"`
	KubeConfig      string `json:"kubeconfig,omitempty"`
	ResyncPeriod    string `json:"resync_period,omitempty"`
	ReconcilePeriod string `json:"reconcile_period,omitempty"`
	CaddyAdminURL   string `json:"caddy_admin_url,omitempty"`
	CaddyServerName string `json:"caddy_server_name,omitempty"`

	// 内部状态（运行时初始化）
	config      *config.Config
	adminClient *router.AdminAPIClient
	tracker     *router.RouteIDTracker
	watcher     *k8s.Watcher
	k8sClient   kubernetes.Interface
	ctx         context.Context
	cancel      context.CancelFunc
	logger      *zap.Logger
}

// CaddyModule 返回模块信息
func (K8sRouter) CaddyModule() caddy.ModuleInfo {
	return caddy.ModuleInfo{
		ID:  "k8s_router",
		New: func() caddy.Module { return new(K8sRouter) },
	}
}

// Provision 初始化模块
func (kr *K8sRouter) Provision(ctx caddy.Context) error {
	kr.logger = ctx.Logger(kr)

	// 构造配置对象
	kr.config = &config.Config{
		Namespace:       kr.Namespace,
		BaseDomain:      kr.BaseDomain,
		DefaultPort:     kr.DefaultPort,
		KubeConfig:      kr.KubeConfig,
		ResyncPeriod:    kr.ResyncPeriod,
		ReconcilePeriod: kr.ReconcilePeriod,
		CaddyAdminURL:   kr.CaddyAdminURL,
		CaddyServerName: kr.CaddyServerName,
	}

	// 验证配置
	if err := kr.config.Validate(); err != nil {
		return err
	}

	kr.logger.Info("K8s router module provisioned",
		zap.String("namespace", kr.config.Namespace),
		zap.String("base_domain", kr.config.BaseDomain),
		zap.Int("default_port", kr.config.DefaultPort),
	)

	return nil
}

// Validate 验证配置
func (kr *K8sRouter) Validate() error {
	if kr.config == nil {
		return nil
	}
	return kr.config.Validate()
}

// Start 启动模块（实现 caddy.App 接口）
func (kr *K8sRouter) Start() error {
	kr.logger.Info("K8s router starting...")

	// 创建 context
	kr.ctx, kr.cancel = context.WithCancel(context.Background())

	// 1. 创建 Kubernetes client
	clientset, err := k8s.NewKubernetesClient(kr.config.KubeConfig)
	if err != nil {
		return err
	}
	kr.k8sClient = clientset

	// 2. 创建 AdminAPIClient
	kr.adminClient = router.NewAdminAPIClient(kr.config.CaddyAdminURL, kr.config.CaddyServerName)

	// 3. 创建 RouteIDTracker
	kr.tracker = router.NewRouteIDTracker()

	// 4. 延迟恢复 Tracker（等待 Caddy Admin API 启动完成）
	go kr.recoverTrackerWithRetry()

	// 5. 创建 EventHandler
	eventHandler := NewEventHandler(
		kr.adminClient,
		kr.tracker,
		clientset,
		kr.config.Namespace,
		kr.config.BaseDomain,
		kr.config.DefaultPort,
		kr.logger,
	)

	// 6. 创建并启动 Watcher
	kr.watcher = k8s.NewWatcher(
		clientset,
		kr.config.Namespace,
		kr.config.GetLabelSelector(), // 使用硬编码的 label selector
		kr.config.GetResyncPeriodDuration(),
		eventHandler,
	)

	// 在后台启动 Watcher
	go func() {
		if err := kr.watcher.Start(kr.ctx); err != nil {
			kr.logger.Error("Watcher stopped with error", zap.Error(err))
		}
	}()

	// 7. 启动时执行一次对账
	go func() {
		if err := kr.reconcileRoutesWithK8s(); err != nil {
			kr.logger.Warn("Initial reconciliation failed", zap.Error(err))
		}
	}()

	// 8. 启动定期对账 goroutine
	go kr.runPeriodicReconciliation()

	kr.logger.Info("K8s router started",
		zap.String("namespace", kr.config.Namespace),
		zap.String("base_domain", kr.config.BaseDomain),
		zap.Duration("reconcile_period", kr.config.GetReconcilePeriodDuration()),
	)

	return nil
}

// Stop 停止模块（实现 caddy.App 接口）
func (kr *K8sRouter) Stop() error {
	kr.logger.Info("K8s router stopping...")

	if kr.cancel != nil {
		kr.cancel()
	}

	if kr.watcher != nil {
		kr.watcher.Stop()
	}

	kr.logger.Info("K8s router stopped")
	return nil
}

// recoverTrackerWithRetry 带重试机制的异步恢复 Tracker
func (kr *K8sRouter) recoverTrackerWithRetry() {
	const (
		maxRetries        = 5
		initialDelay      = 2 * time.Second
		maxDelay          = 30 * time.Second
		healthCheckURL    = "/config/"
		healthCheckTimeout = 2 * time.Second // 快速健康检查,避免阻塞
	)

	kr.logger.Info("Starting delayed tracker recovery...")

	// 首次延迟,等待 Caddy Admin API 启动
	time.Sleep(initialDelay)

	delay := initialDelay
	for attempt := 1; attempt <= maxRetries; attempt++ {
		kr.logger.Info("Attempting to recover tracker",
			zap.Int("attempt", attempt),
			zap.Int("max_retries", maxRetries),
		)

		// 健康检查:先测试 Admin API 是否可访问(使用短超时快速失败)
		ctx, cancel := context.WithTimeout(context.Background(), healthCheckTimeout)
		if err := kr.adminClient.HealthCheck(ctx, healthCheckURL); err != nil {
			cancel()
			kr.logger.Warn("Admin API health check failed",
				zap.Int("attempt", attempt),
				zap.Error(err),
				zap.Duration("retry_after", delay),
			)

			if attempt < maxRetries {
				time.Sleep(delay)
				// 指数退避,但不超过 maxDelay
				delay *= 2
				if delay > maxDelay {
					delay = maxDelay
				}
			}
			continue
		}
		cancel()

		// Admin API 健康,先清理重复路由
		ctx2, cancel2 := context.WithTimeout(context.Background(), 15*time.Second)
		deletedCount, err := kr.adminClient.CleanupDuplicateRoutes(ctx2)
		cancel2()

		if err != nil {
			kr.logger.Warn("Failed to cleanup duplicate routes",
				zap.Int("attempt", attempt),
				zap.Error(err),
			)
			// 清理失败不阻塞恢复流程,继续尝试恢复
		} else if deletedCount > 0 {
			kr.logger.Info("Cleaned up duplicate routes",
				zap.Int("deleted_count", deletedCount),
			)
		}

		// 尝试恢复 Tracker
		// 注意：不再创建基础路由，它们由 Caddyfile 定义
		if err := kr.recoverTracker(); err != nil {
			kr.logger.Warn("Failed to recover tracker",
				zap.Int("attempt", attempt),
				zap.Error(err),
			)

			if attempt < maxRetries {
				time.Sleep(delay)
				delay *= 2
				if delay > maxDelay {
					delay = maxDelay
				}
			}
			continue
		}

		// 恢复成功
		kr.logger.Info("Tracker recovery completed successfully",
			zap.Int("attempt", attempt),
		)
		return
	}

	// 所有重试都失败
	kr.logger.Error("Failed to recover tracker after all retries",
		zap.Int("max_retries", maxRetries),
	)
}

// recoverTracker 从 Caddy Admin API 和 K8s 恢复 RouteIDTracker
// 参考 gitness 的修复思路：不从 routeID 反推，而是通过 K8s Deployments 匹配
// 使用 gitspace identifier（来自 deployment label）而不是 deployment name
//
// 简化架构：不再管理基础路由（healthz, catch-all）
// 基础路由由 Caddyfile 定义，插件只管理动态 deployment 路由
func (kr *K8sRouter) recoverTracker() error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// 1. 从 Caddy 获取所有路由
	routes, err := kr.adminClient.ListRoutes(ctx)
	if err != nil {
		return err
	}

	// 构建 routeID -> route 映射（只关注有 ID 的动态路由）
	routeMap := make(map[string]*router.RouteConfig)
	for _, route := range routes {
		// 只处理有 @id 的路由（动态创建的 deployment 路由）
		// Caddyfile 创建的路由没有 @id，我们不管理它们
		if route.ID != "" {
			routeMap[route.ID] = route
		}
	}

	// 2. 从 K8s 获取所有 Deployments
	deployments, err := kr.k8sClient.AppsV1().Deployments(kr.config.Namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("failed to list deployments: %w", err)
	}

	// 3. 遍历 Deployments，恢复 tracker 映射
	recoveredCount := 0
	skippedCount := 0
	for i := range deployments.Items {
		deployment := &deployments.Items[i]

		// 从 deployment labels 获取 gitspace identifier
		gitspaceIdentifier := k8s.GetGitspaceIdentifier(deployment)
		if gitspaceIdentifier == "" {
			kr.logger.Debug("Deployment missing gitspace identifier, skipping recovery",
				zap.String("deployment", deployment.Name),
			)
			skippedCount++
			continue
		}

		// 使用 gitspace identifier 构造期望的 routeID
		routeID := router.BuildRouteID(gitspaceIdentifier)

		// 检查 Caddy 中是否存在对应的路由
		if route, exists := routeMap[routeID]; exists {
			deploymentKey := fmt.Sprintf("%s/%s", deployment.Namespace, deployment.Name)
			kr.tracker.Set(deploymentKey, route.ID, route.TargetAddr)
			kr.logger.Info("Recovered route",
				zap.String("route_id", route.ID),
				zap.String("deployment", deployment.Name),
				zap.String("gitspace_identifier", gitspaceIdentifier),
				zap.String("deployment_key", deploymentKey),
				zap.String("target_addr", route.TargetAddr),
			)
			recoveredCount++
		}
	}

	kr.logger.Info("Tracker recovered",
		zap.Int("total_routes", len(routes)),
		zap.Int("recovered_mappings", recoveredCount),
		zap.Int("skipped_deployments", skippedCount),
	)

	return nil
}

// reconcileRoutesWithK8s 全量对账 Caddy 路由与 K8s Deployment 状态
// 简化架构：只处理动态 deployment 路由，不管理 Caddyfile 定义的基础路由
func (kr *K8sRouter) reconcileRoutesWithK8s() error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	kr.logger.Info("Starting route reconciliation...")

	// 1. 获取 Caddy 中所有管理的路由（只包含有 @id 的动态路由）
	routes, err := kr.adminClient.ListRoutes(ctx)
	if err != nil {
		kr.logger.Error("Failed to list Caddy routes during reconciliation", zap.Error(err))
		return err
	}

	// 构建 Caddy 路由集合 (routeID -> route)
	// IsManagedRouteID 会过滤掉 Caddyfile 路由（它们没有 @id 或不符合命名规则）
	caddyRoutes := make(map[string]*router.RouteConfig)
	for _, route := range routes {
		if router.IsManagedRouteID(route.ID) {
			caddyRoutes[route.ID] = route
		}
	}

	// 2. 获取 K8s 中所有符合条件的 Deployment (replicas=1 && ready)
	deployments, err := kr.k8sClient.AppsV1().Deployments(kr.config.Namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		kr.logger.Error("Failed to list K8s deployments during reconciliation", zap.Error(err))
		return err
	}

	// 构建期望的路由集合
	expectedRoutes := make(map[string]bool)
	// gitspaceIdentifierToDeploymentKey 映射，用于清理时查找 deploymentKey
	gitspaceIdentifierToDeploymentKey := make(map[string]string)

	for i := range deployments.Items {
		deployment := &deployments.Items[i]

		// 只处理单副本 Deployment
		replicas := k8s.DesiredReplicaCount(deployment)
		if replicas != 1 {
			continue
		}

		// 只处理就绪的 Deployment
		if !isDeploymentReady(deployment) {
			continue
		}

		// 使用 gitspaceIdentifier 而不是 deployment.Name
		gitspaceIdentifier := k8s.GetGitspaceIdentifier(deployment)
		if gitspaceIdentifier == "" {
			kr.logger.Warn("Deployment missing gitspace identifier, skipping",
				zap.String("deployment", deployment.Name),
			)
			continue
		}

		routeID := router.BuildRouteID(gitspaceIdentifier)
		expectedRoutes[routeID] = true

		// 记录映射关系，用于后续清理 tracker
		deploymentKey := fmt.Sprintf("%s/%s", deployment.Namespace, deployment.Name)
		gitspaceIdentifierToDeploymentKey[gitspaceIdentifier] = deploymentKey
	}

	// 3. 删除 Caddy 中存在但 K8s 中不存在的路由（清理孤立路由）
	deletedCount := 0
	for routeID := range caddyRoutes {
		if !expectedRoutes[routeID] {
			kr.logger.Info("Reconciliation: deleting orphaned route",
				zap.String("route_id", routeID),
			)

			if err := kr.adminClient.DeleteRoute(ctx, routeID); err != nil {
				kr.logger.Warn("Failed to delete orphaned route during reconciliation",
					zap.String("route_id", routeID),
					zap.Error(err),
				)
			} else {
				// 从 tracker 中清理
				// 从 routeID 解析出 gitspaceIdentifier
				gitspaceIdentifier, err := router.ParseRouteID(routeID)
				if err == nil {
					// 使用映射找到对应的 deploymentKey
					if deploymentKey, exists := gitspaceIdentifierToDeploymentKey[gitspaceIdentifier]; exists {
						kr.tracker.Delete(deploymentKey)
					}
				}
				deletedCount++
			}
		}
	}

	// 4. 对于 K8s 中存在但 Caddy 中缺失的路由，由 Informer 的 resync 机制自动创建
	// 这里不主动创建，避免与事件处理冲突

	kr.logger.Info("Route reconciliation completed",
		zap.Int("caddy_routes", len(caddyRoutes)),
		zap.Int("expected_routes", len(expectedRoutes)),
		zap.Int("deleted_orphaned", deletedCount),
	)

	return nil
}

// runPeriodicReconciliation 定期执行对账
func (kr *K8sRouter) runPeriodicReconciliation() {
	ticker := time.NewTicker(kr.config.GetReconcilePeriodDuration())
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			kr.logger.Debug("Running periodic reconciliation...")
			if err := kr.reconcileRoutesWithK8s(); err != nil {
				kr.logger.Warn("Periodic reconciliation failed", zap.Error(err))
			}
		case <-kr.ctx.Done():
			kr.logger.Info("Stopping periodic reconciliation")
			return
		}
	}
}

// UnmarshalCaddyfile 支持 Caddyfile 配置格式
func (kr *K8sRouter) UnmarshalCaddyfile(d *caddyfile.Dispenser) error {
	// 跳过指令名称
	d.Next()

	// 解析块内容
	for d.NextBlock(0) {
		switch d.Val() {
		case "namespace":
			if !d.NextArg() {
				return d.ArgErr()
			}
			kr.Namespace = d.Val()

		case "base_domain":
			if !d.NextArg() {
				return d.ArgErr()
			}
			kr.BaseDomain = d.Val()

		case "default_port":
			if !d.NextArg() {
				return d.ArgErr()
			}
			port, err := strconv.Atoi(d.Val())
			if err != nil {
				return d.Errf("invalid default_port: %v", err)
			}
			kr.DefaultPort = port

		case "kubeconfig":
			if !d.NextArg() {
				return d.ArgErr()
			}
			kr.KubeConfig = d.Val()

		case "resync_period":
			if !d.NextArg() {
				return d.ArgErr()
			}
			kr.ResyncPeriod = d.Val()

		case "reconcile_period":
			if !d.NextArg() {
				return d.ArgErr()
			}
			kr.ReconcilePeriod = d.Val()

		case "caddy_admin_url":
			if !d.NextArg() {
				return d.ArgErr()
			}
			kr.CaddyAdminURL = d.Val()

		case "caddy_server_name":
			if !d.NextArg() {
				return d.ArgErr()
			}
			kr.CaddyServerName = d.Val()

		default:
			return d.Errf("unrecognized subdirective: %s", d.Val())
		}
	}

	return nil
}

// Interface guards
var (
	_ caddy.Provisioner     = (*K8sRouter)(nil)
	_ caddy.Validator       = (*K8sRouter)(nil)
	_ caddy.App             = (*K8sRouter)(nil)
	_ caddyfile.Unmarshaler = (*K8sRouter)(nil)
)
