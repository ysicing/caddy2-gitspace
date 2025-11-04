package main

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/caddy/v2/caddyconfig/caddyfile"
	"github.com/ysicing/caddy2-k8s/config"
	"github.com/ysicing/caddy2-k8s/k8s"
	"github.com/ysicing/caddy2-k8s/router"
	"go.uber.org/zap"
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
	CaddyAdminURL   string `json:"caddy_admin_url,omitempty"`
	CaddyServerName string `json:"caddy_server_name,omitempty"`

	// 内部状态（运行时初始化）
	config      *config.Config
	adminClient *router.AdminAPIClient
	tracker     *router.RouteIDTracker
	watcher     *k8s.Watcher
	ctx         context.Context
	cancel      context.CancelFunc
	logger      *zap.Logger
}

// CaddyModule 返回模块信息
func (K8sRouter) CaddyModule() caddy.ModuleInfo {
	return caddy.ModuleInfo{
		ID:  "http.handlers.k8s_router",
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

	// 2. 创建 AdminAPIClient
	kr.adminClient = router.NewAdminAPIClient(kr.config.CaddyAdminURL, kr.config.CaddyServerName)

	// 3. 创建 RouteIDTracker
	kr.tracker = router.NewRouteIDTracker()

	// 4. 恢复 Tracker（从 Caddy 查询现有路由）
	if err := kr.recoverTracker(); err != nil {
		kr.logger.Warn("Failed to recover tracker", zap.Error(err))
		// 不返回错误，继续启动
	}

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
		kr.config.GetResyncPeriodDuration(),
		eventHandler,
	)

	// 在后台启动 Watcher
	go func() {
		if err := kr.watcher.Start(kr.ctx); err != nil {
			kr.logger.Error("Watcher stopped with error", zap.Error(err))
		}
	}()

	kr.logger.Info("K8s router started",
		zap.String("namespace", kr.config.Namespace),
		zap.String("base_domain", kr.config.BaseDomain),
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

// recoverTracker 从 Caddy Admin API 恢复 RouteIDTracker
func (kr *K8sRouter) recoverTracker() error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	routeIDs, err := kr.adminClient.ListRoutes(ctx)
	if err != nil {
		return err
	}

	// 从 routeID 反推 deploymentKey
	// routeID 格式: k8s-{namespace}-{deployment-name}
	for _, routeID := range routeIDs {
		// 提取 namespace 和 deployment name
		// k8s-default-vscode -> default/vscode
		parts := strings.SplitN(routeID, "-", 3) // ["k8s", "default", "vscode"]
		if len(parts) == 3 {
			deploymentKey := fmt.Sprintf("%s/%s", parts[1], parts[2])
			kr.tracker.Set(deploymentKey, routeID)
			kr.logger.Info("Recovered route",
				zap.String("route_id", routeID),
				zap.String("deployment_key", deploymentKey),
			)
		}
	}

	kr.logger.Info("Tracker recovered",
		zap.Int("routes", len(routeIDs)),
	)

	return nil
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
	_ caddy.Provisioner       = (*K8sRouter)(nil)
	_ caddy.Validator         = (*K8sRouter)(nil)
	_ caddyfile.Unmarshaler   = (*K8sRouter)(nil)
)
