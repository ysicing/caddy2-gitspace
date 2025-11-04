package config

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"
)

// Config 定义插件配置
type Config struct {
	// Namespace 监听的 Kubernetes 命名空间
	Namespace string `json:"namespace"`

	// BaseDomain 基础域名（如 "example.com"）
	BaseDomain string `json:"base_domain"`

	// DefaultPort 默认端口（当 Deployment 缺少端口注解时使用）
	DefaultPort int `json:"default_port,omitempty"`

	// KubeConfig Kubernetes 配置文件路径（集群外运行时需要）
	KubeConfig string `json:"kubeconfig,omitempty"`

	// ResyncPeriod Informer 重新同步周期
	ResyncPeriod string `json:"resync_period,omitempty"`

	// ReconcilePeriod 全量对账周期
	ReconcilePeriod string `json:"reconcile_period,omitempty"`

	// CaddyAdminURL Caddy Admin API 地址
	CaddyAdminURL string `json:"caddy_admin_url,omitempty"`

	// CaddyServerName Caddy Server 名称
	CaddyServerName string `json:"caddy_server_name,omitempty"`
}

// Validate 验证配置有效性
func (c *Config) Validate() error {
	// 验证必需字段
	if c.Namespace == "" {
		return fmt.Errorf("namespace is required")
	}

	if c.BaseDomain == "" {
		return fmt.Errorf("base_domain is required")
	}

	// 验证基础域名格式
	if strings.Contains(c.BaseDomain, "://") {
		return fmt.Errorf("base_domain should not contain protocol (http:// or https://)")
	}

	// 验证端口范围
	if c.DefaultPort != 0 {
		if c.DefaultPort < 1 || c.DefaultPort > 65535 {
			return fmt.Errorf("default_port must be between 1 and 65535, got %d", c.DefaultPort)
		}
	} else {
		// 设置默认端口
		c.DefaultPort = 8089
	}

	// 验证 ResyncPeriod 格式
	if c.ResyncPeriod != "" {
		if _, err := time.ParseDuration(c.ResyncPeriod); err != nil {
			return fmt.Errorf("invalid resync_period format: %w", err)
		}
	} else {
		// 设置默认重新同步周期
		c.ResyncPeriod = "30s"
	}

	// 验证 ReconcilePeriod 格式
	if c.ReconcilePeriod != "" {
		if _, err := time.ParseDuration(c.ReconcilePeriod); err != nil {
			return fmt.Errorf("invalid reconcile_period format: %w", err)
		}
	} else {
		// 设置默认对账周期为 5 分钟
		c.ReconcilePeriod = "5m"
	}

	// 验证 Caddy Admin URL
	if c.CaddyAdminURL != "" {
		if _, err := url.Parse(c.CaddyAdminURL); err != nil {
			return fmt.Errorf("invalid caddy_admin_url: %w", err)
		}
	} else {
		// 设置默认 Admin API 地址
		c.CaddyAdminURL = "http://localhost:2019"
	}

	// 设置默认 Server 名称
	if c.CaddyServerName == "" {
		c.CaddyServerName = "srv0"
	}

	return nil
}

// Load 从 JSON 加载配置
func Load(data []byte) (*Config, error) {
	var config Config
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse config: %w", err)
	}

	if err := config.Validate(); err != nil {
		return nil, err
	}

	return &config, nil
}

// GetResyncPeriodDuration 返回解析后的重新同步周期
func (c *Config) GetResyncPeriodDuration() time.Duration {
	duration, _ := time.ParseDuration(c.ResyncPeriod)
	return duration
}

// GetReconcilePeriodDuration 返回解析后的对账周期
func (c *Config) GetReconcilePeriodDuration() time.Duration {
	duration, _ := time.ParseDuration(c.ReconcilePeriod)
	return duration
}

// GetLabelSelector 返回硬编码的 Label Selector
// 固定为 "gitspace.app.io/managed-by=caddy"
func (c *Config) GetLabelSelector() string {
	return "gitspace.app.io/managed-by=caddy"
}
