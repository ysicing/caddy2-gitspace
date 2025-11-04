# 接口契约：Caddy Admin API Client 接口

**组件**：Caddy Admin API Client
**职责**：通过 Caddy Admin API 管理路由，实现路由的创建、删除和查询操作

---

## 接口定义

```go
package router

import (
    "context"
)

// AdminAPIClient 封装 Caddy Admin API 调用
type AdminAPIClient interface {
    // CreateRoute 通过 Admin API 创建路由
    // routeID: 路由唯一标识，格式 "k8s-{namespace}-{deployment-name}"
    // domain: 完整域名（如 "app.example.com"）
    // targetIP: 目标 Pod IP
    // targetPort: 目标端口
    // 返回 error 如果 API 调用失败或参数无效
    CreateRoute(ctx context.Context, routeID, domain, targetIP string, targetPort int) error

    // DeleteRoute 通过 Admin API 删除路由
    // routeID: 路由唯一标识
    // 如果路由不存在（404），不返回错误（幂等）
    DeleteRoute(ctx context.Context, routeID string) error

    // GetRoute 查询路由配置（可选，用于调试）
    // routeID: 路由唯一标识
    // 返回路由配置或 nil（不存在）
    GetRoute(ctx context.Context, routeID string) (*RouteConfig, error)

    // ListRoutes 列出所有 k8s-* 路由（用于恢复 RouteIDTracker）
    // 返回路由 ID 列表
    ListRoutes(ctx context.Context) ([]string, error)
}

// RouteConfig 路由配置（从 Caddy 返回）
type RouteConfig struct {
    ID         string   // @id
    Domain     string   // match.host[0]
    TargetAddr string   // upstreams[0].dial (格式: "ip:port")
}
```

---

## 契约测试

### 测试 1：CreateRoute 成功

**前置条件**：
- Caddy Admin API 可访问（http://localhost:2019）
- Server "srv0" 已配置

**操作**：
```go
client := NewAdminAPIClient("http://localhost:2019", "srv0")
err := client.CreateRoute(
    ctx,
    "k8s-default-test-app",
    "test-app.example.com",
    "10.0.0.1",
    8080,
)
```

**预期结果**：
- `err == nil`
- Caddy 配置中新增路由 `@id:k8s-default-test-app`
- 验证：`curl http://localhost:2019/config/apps/http/servers/srv0/routes/@id:k8s-default-test-app` 返回 200

**HTTP 请求验证**：
```bash
# POST /config/apps/http/servers/srv0/routes
# Body:
{
  "@id": "k8s-default-test-app",
  "match": [{"host": ["test-app.example.com"]}],
  "handle": [{
    "handler": "reverse_proxy",
    "upstreams": [{"dial": "10.0.0.1:8080"}]
  }]
}
```

---

### 测试 2：CreateRoute 覆盖现有路由（Pod IP 变化）

**前置条件**：
- 路由 "k8s-default-test-app" 已存在（IP: 10.0.0.1）

**操作**：
```go
err := client.CreateRoute(
    ctx,
    "k8s-default-test-app",
    "test-app.example.com",
    "10.0.0.2",  // 新 Pod IP
    8080,
)
```

**预期结果**：
- `err == nil`
- 路由配置更新为新 IP
- 验证：`GetRoute("k8s-default-test-app")` 返回 "10.0.0.2:8080"

---

### 测试 3：DeleteRoute 成功

**前置条件**：
- 路由 "k8s-default-test-app" 已存在

**操作**：
```go
err := client.DeleteRoute(ctx, "k8s-default-test-app")
```

**预期结果**：
- `err == nil`
- Caddy 配置中路由已删除
- 验证：`curl http://localhost:2019/config/apps/http/servers/srv0/routes/@id:k8s-default-test-app` 返回 404

**HTTP 请求验证**：
```bash
# DELETE /config/apps/http/servers/srv0/routes/@id:k8s-default-test-app
```

---

### 测试 4：DeleteRoute 幂等性（路由不存在）

**前置条件**：
- 路由 "k8s-default-nonexistent" 不存在

**操作**：
```go
err := client.DeleteRoute(ctx, "k8s-default-nonexistent")
```

**预期结果**：
- `err == nil`（幂等，404 不视为错误）

---

### 测试 5：GetRoute 查询成功

**前置条件**：
- 路由 "k8s-default-test-app" 已存在

**操作**：
```go
config, err := client.GetRoute(ctx, "k8s-default-test-app")
```

**预期结果**：
- `err == nil`
- `config.ID == "k8s-default-test-app"`
- `config.Domain == "test-app.example.com"`
- `config.TargetAddr == "10.0.0.1:8080"`

---

### 测试 6：GetRoute 路由不存在

**操作**：
```go
config, err := client.GetRoute(ctx, "k8s-default-nonexistent")
```

**预期结果**：
- `err == nil`
- `config == nil`

---

### 测试 7：ListRoutes 返回所有 k8s-* 路由

**前置条件**：
- 已创建路由：
  - k8s-default-app1
  - k8s-default-app2
  - manual-route（手动创建，非 k8s-*）

**操作**：
```go
routeIDs, err := client.ListRoutes(ctx)
```

**预期结果**：
- `err == nil`
- `len(routeIDs) == 2`
- `routeIDs` 包含 "k8s-default-app1" 和 "k8s-default-app2"
- 不包含 "manual-route"

**用途**：插件启动时恢复 RouteIDTracker

---

## 错误场景

### 错误 1：CreateRoute 参数无效（空 IP）

**操作**：
```go
err := client.CreateRoute(
    ctx,
    "k8s-default-test-app",
    "test-app.example.com",
    "",  // 空 IP
    8080,
)
```

**预期结果**：
- `err != nil`
- 错误消息包含 "invalid IP"
- 不调用 Caddy Admin API

---

### 错误 2：CreateRoute 端口超范围

**操作**：
```go
err := client.CreateRoute(
    ctx,
    "k8s-default-test-app",
    "test-app.example.com",
    "10.0.0.1",
    70000,  // 超过 65535
)
```

**预期结果**：
- `err != nil`
- 错误消息包含 "port out of range"
- 不调用 Caddy Admin API

---

### 错误 3：Caddy Admin API 不可达

**前置条件**：
- Caddy Admin API 未运行或地址错误

**操作**：
```go
client := NewAdminAPIClient("http://localhost:9999", "srv0")
err := client.CreateRoute(ctx, "k8s-default-test-app", "test-app.example.com", "10.0.0.1", 8080)
```

**预期结果**：
- `err != nil`
- 错误消息包含 "connection refused" 或 "timeout"
- 记录错误日志

---

### 错误 4：Caddy 返回 500 Internal Server Error

**前置条件**：
- Caddy Admin API 配置错误

**操作**：
```go
err := client.CreateRoute(ctx, "k8s-default-test-app", "test-app.example.com", "10.0.0.1", 8080)
```

**预期结果**：
- `err != nil`
- 错误消息包含 "Caddy Admin API error: 500"
- 记录 Caddy 返回的错误响应

---

### 错误 5：上下文超时

**操作**：
```go
ctx, cancel := context.WithTimeout(context.Background(), 1*time.Millisecond)
defer cancel()

err := client.CreateRoute(ctx, "k8s-default-test-app", "test-app.example.com", "10.0.0.1", 8080)
```

**预期结果**：
- `err == context.DeadlineExceeded`

---

## 并发安全测试

### 测试 8：并发调用 CreateRoute

**操作**：
```go
var wg sync.WaitGroup

// 10 个 goroutines 并发创建路由
for i := 0; i < 10; i++ {
    wg.Add(1)
    go func(id int) {
        defer wg.Done()
        client.CreateRoute(
            ctx,
            fmt.Sprintf("k8s-default-app-%d", id),
            fmt.Sprintf("app-%d.example.com", id),
            "10.0.0.1",
            8080,
        )
    }(i)
}

wg.Wait()
```

**预期结果**：
- 无数据竞争（`go test -race` 通过）
- 所有路由创建成功
- Caddy 配置中有 10 个路由

---

### 测试 9：并发读写操作

**操作**：
```go
var wg sync.WaitGroup

// 5 个写操作
for i := 0; i < 5; i++ {
    wg.Add(1)
    go func(id int) {
        defer wg.Done()
        client.CreateRoute(ctx, fmt.Sprintf("k8s-default-app-%d", id), fmt.Sprintf("app-%d.example.com", id), "10.0.0.1", 8080)
    }(i)
}

// 20 个读操作
for i := 0; i < 20; i++ {
    wg.Add(1)
    go func() {
        defer wg.Done()
        client.GetRoute(ctx, "k8s-default-app-1")
    }()
}

wg.Wait()
```

**预期结果**：
- 无数据竞争
- 所有操作成功完成

---

## 性能要求

- **HTTP 请求延迟**：`CreateRoute()/DeleteRoute()` < 50ms（正常网络）
- **批量查询**：`ListRoutes()` < 100ms（100 路由）
- **超时设置**：默认 5s（可配置）
- **重试策略**：支持指数退避重试（可选）

---

## 可观测性

AdminAPIClient 应该提供：
- **日志记录**：
  - INFO：路由创建/删除成功
  - WARN：Admin API 返回 4xx
  - ERROR：Admin API 不可达或返回 5xx

- **Prometheus 指标**（可选）：
  - `caddy_admin_api_requests_total{operation="create|delete|get|list", status="success|failure"}`：API 调用次数
  - `caddy_admin_api_duration_seconds{operation}`：API 调用延迟

---

## 实现建议

### HTTP 客户端配置

```go
type AdminAPIClient struct {
    baseURL    string           // http://localhost:2019
    serverName string           // srv0
    httpClient *http.Client
}

func NewAdminAPIClient(baseURL, serverName string) *AdminAPIClient {
    return &AdminAPIClient{
        baseURL:    strings.TrimSuffix(baseURL, "/"),
        serverName: serverName,
        httpClient: &http.Client{
            Timeout: 5 * time.Second,
            Transport: &http.Transport{
                MaxIdleConns:        10,
                IdleConnTimeout:     30 * time.Second,
                DisableKeepAlives:   false,
            },
        },
    }
}
```

### 重试逻辑（可选）

```go
func (c *AdminAPIClient) CreateRoute(ctx context.Context, routeID, domain, targetIP string, targetPort int) error {
    return retry.Do(
        func() error {
            return c.createRouteOnce(ctx, routeID, domain, targetIP, targetPort)
        },
        retry.Context(ctx),
        retry.Attempts(3),
        retry.Delay(100*time.Millisecond),
        retry.DelayType(retry.BackOffDelay),
        retry.RetryIf(func(err error) bool {
            // 只重试临时错误（网络超时、5xx）
            return isRetryableError(err)
        }),
    )
}
```

---

## 与 RouteIDTracker 的协作

AdminAPIClient 不维护状态，只负责 API 调用。RouteIDTracker 负责维护 `deploymentKey → routeID` 映射。

**典型工作流**：

```go
// Watcher 发现 Deployment 就绪
deploymentKey := "default/vscode"
routeID := fmt.Sprintf("k8s-%s", strings.ReplaceAll(deploymentKey, "/", "-"))
domain := "vscode.example.com"
podIP := "10.0.0.1"
port := 8080

// 1. 调用 Admin API 创建路由
err := adminClient.CreateRoute(ctx, routeID, domain, podIP, port)
if err != nil {
    return err
}

// 2. 记录到 Tracker
tracker.Set(deploymentKey, routeID)

// 3. 写回注解到 Deployment
updateDeploymentAnnotation(deploymentKey, domain)
```

**删除工作流**：

```go
// Watcher 发现 Deployment 删除
deploymentKey := "default/vscode"

// 1. 从 Tracker 查找 Route ID
routeID, exists := tracker.Get(deploymentKey)
if !exists {
    return nil  // 没有路由，跳过
}

// 2. 调用 Admin API 删除路由
err := adminClient.DeleteRoute(ctx, routeID)
if err != nil {
    return err
}

// 3. 清理 Tracker
tracker.Delete(deploymentKey)
```

---

## 总结

AdminAPIClient 是一个轻量级 HTTP 客户端，负责：
- ✅ 封装 Caddy Admin API 调用
- ✅ 参数验证
- ✅ 错误处理和日志记录
- ❌ 不维护路由状态（由 Caddy 管理）
- ❌ 不维护 Deployment 映射（由 RouteIDTracker 管理）
