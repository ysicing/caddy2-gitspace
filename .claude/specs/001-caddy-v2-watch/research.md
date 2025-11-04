# 研究文档：Caddy v2 Kubernetes Deployment 动态路由扩展

**日期**：2025-11-04
**功能**：001-caddy-v2-watch

## 研究概述

本文档记录了实施 Caddy v2 Kubernetes 动态路由扩展所需的技术研究和决策。

---

## 1. Caddy v2 扩展开发模式

###  决策
使用 Caddy v2 模块系统开发扩展，编译为 Caddy 模块，通过 Caddyfile 指令配置。

### 理由
- Caddy v2 采用模块化架构，所有功能通过模块注册
- 作为模块运行，无需独立进程管理（Sidecar）
- 可以使用 Caddy 的 Admin API 进行路由管理
- 支持热重载配置，无需重启服务
- 部署简单：使用 xcaddy 编译即可

### 考虑的替代方案
- **独立进程 + Admin API**：被拒绝，需要额外的进程管理和通信复杂度
- **修改 Caddy 核心代码**：被拒绝，违反扩展原则，难以维护和升级
- **使用 Caddyfile 配置**：被拒绝，需要动态路由，静态配置无法满足需求

### 关键API
```go
// 模块接口
type Module interface {
    CaddyModule() ModuleInfo
}

// HTTP App 模块（用于注册路由管理逻辑）
type App interface {
    Start() error
    Stop() error
}

// 配置器接口
type Provisioner interface {
    Provision(Context) error
}

// 验证器接口
type Validator interface {
    Validate() error
}
```

### 模块注册示例
```go
func init() {
    caddy.RegisterModule(K8sRouter{})
}

func (K8sRouter) CaddyModule() caddy.ModuleInfo {
    return caddy.ModuleInfo{
        ID:  "http.handlers.k8s_router",  // 或 apps.k8s_router
        New: func() caddy.Module { return new(K8sRouter) },
    }
}
```

**注意**：插件作为 Caddy App 运行，不实现 HTTP handler 接口（不处理用户请求），只通过 Admin API 管理路由。

---

## 2. Kubernetes Client-go Watch 机制

### 决策
使用 client-go 的 Informer 模式监听 Deployment 和 Pod 资源变化。

### 理由
- Informer 提供本地缓存，减少 API Server 负载
- 自动处理重连和资源版本管理
- 支持事件回调机制（Add/Update/Delete）
- 内置错误重试和指数退避

### 考虑的替代方案
- **直接使用 Watch API**：被拒绝，需要手动处理重连、缓存和错误，复杂度高
- **定时轮询 List**：被拒绝，延迟高、效率低、API Server 负载大

### 关键API
```go
// SharedInformerFactory 创建 informers
factory := informers.NewSharedInformerFactoryWithOptions(
    client,
    resyncPeriod,
    informers.WithNamespace(namespace),
)

// Deployment Informer
deployInformer := factory.Apps().V1().Deployments().Informer()
deployInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
    AddFunc:    handleDeploymentAdd,
    UpdateFunc: handleDeploymentUpdate,
    DeleteFunc: handleDeploymentDelete,
})

// Pod Informer (监听 Pod 就绪状态)
podInformer := factory.Core().V1().Pods().Informer()
```

---

## 3. Deployment 注解读写

### 决策
使用 Kubernetes client-go 的 PATCH 操作更新 Deployment 注解。

### 理由
- PATCH 只更新需要修改的字段，避免冲突
- 支持 Strategic Merge Patch，适合注解更新
- 原子操作，保证并发安全

### 考虑的替代方案
- **UPDATE 整个 Deployment**：被拒绝，可能与其他控制器冲突，需要处理 resourceVersion
- **使用 ConfigMap 存储路由信息**：被拒绝，增加复杂度，不符合 Kubernetes 最佳实践

### 示例代码
```go
// 读取注解
port := deployment.Annotations["gitspace.caddy.default.port"]

// 写回域名注解
patch := map[string]any{
    "metadata": map[string]any{
        "annotations": map[string]string{
            "gitspace.caddy.route.url": fmt.Sprintf("%s.%s", name, baseDomain),
        },
    },
}
patchBytes, _ := json.Marshal(patch)
client.AppsV1().Deployments(namespace).Patch(
    ctx, name, types.StrategicMergePatchType, patchBytes, metav1.PatchOptions{},
)
```

---

## 4. Pod 就绪状态检测

### 决策
检查 Pod 的 `Status.Conditions` 中 `Ready` 条件为 `True`。

### 理由
- Kubernetes 标准就绪检查机制
- 反映容器的健康检查状态
- 与 Service 的 Endpoint 选择逻辑一致

### 考虑的替代方案
- **检查 Pod Phase**：被拒绝，Phase=Running 不代表应用已就绪
- **自定义健康检查**：被拒绝，增加复杂度，与 K8s 生态不一致

### 示例代码
```go
func isPodReady(pod *corev1.Pod) bool {
    for _, cond := range pod.Status.Conditions {
        if cond.Type == corev1.PodReady {
            return cond.Status == corev1.ConditionTrue
        }
    }
    return false
}
```

---

## 5. Caddy 动态路由配置

### 决策
使用 Caddy Admin API 动态添加/删除路由，插件不维护路由表。

### 理由
- 路由管理是 Caddy 的核心职责，不应重复实现
- Admin API 支持运行时修改配置，无需重启
- 可以精确控制单个路由的生命周期
- 配置变更即时生效
- 利用 Caddy 的所有反向代理特性（健康检查、负载均衡、TLS 等）

### 考虑的替代方案
- **插件维护路由表 + 实现 HTTP handler**：被拒绝，违反单一职责原则，重复实现反向代理逻辑
- **重新加载整个 Caddyfile**：被拒绝，影响已有路由，重载开销大
- **使用 reverse_proxy 指令 + 文件配置**：被拒绝，需要文件系统操作，不够灵活

### 核心实现
通过 AdminAPIClient 调用 Caddy Admin API：
```go
type AdminAPIClient struct {
    baseURL    string       // http://localhost:2019
    serverName string       // srv0
    httpClient *http.Client
}

func (c *AdminAPIClient) CreateRoute(ctx context.Context, routeID, domain, targetIP string, targetPort int) error {
    // POST /config/apps/http/servers/srv0/routes
    // Body: {"@id": routeID, "match": [{"host": [domain]}], "handle": [...]}
}

func (c *AdminAPIClient) DeleteRoute(ctx context.Context, routeID string) error {
    // DELETE /config/apps/http/servers/srv0/routes/@id:{routeID}
}
```

插件只维护轻量级的 Route ID 映射：
```go
type RouteIDTracker struct {
    routes map[string]string  // deploymentKey → routeID
    mu     sync.RWMutex
}
```

**工作流**：
```
K8s Event → Watcher → EventHandler → AdminAPIClient → Caddy Admin API → Caddy 配置更新
                                    ↓
                              RouteIDTracker (记录映射)
```

**HTTP 请求处理**：完全由 Caddy 处理，插件不参与。

---

## 6. 配置管理

### 决策
使用 Caddy JSON 配置 + 环境变量方式配置扩展。

### 理由
- Caddy 原生支持 JSON 配置
- 环境变量适合容器化部署
- 配置验证在 Provision 阶段完成

### 配置项
```json
{
  "apps": {
    "http": {
      "servers": {
        "k8s_router": {
          "listen": [":80"],
          "routes": [{
            "handle": [{
              "@type": "k8s_router",
              "namespace": "default",
              "base_domain": "example.com",
              "default_port": 8089,
              "kubeconfig": "/path/to/kubeconfig"
            }]
          }]
        }
      }
    }
  }
}
```

### 环境变量
- `K8S_NAMESPACE`：监听的命名空间
- `K8S_BASE_DOMAIN`：基础域名
- `K8S_DEFAULT_PORT`：默认端口（默认 8089）
- `KUBECONFIG`：K8s 配置文件路径（集群内可选）

---

## 7. RBAC 权限需求

### 决策
创建 ServiceAccount、Role 和 RoleBinding，授予必要的最小权限。

### 所需权限
```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: caddy-k8s-router
  namespace: <target-namespace>
rules:
- apiGroups: ["apps"]
  resources: ["deployments"]
  verbs: ["get", "list", "watch", "patch"]
- apiGroups: [""]
  resources: ["pods"]
  verbs: ["get", "list", "watch"]
```

### 理由
- 最小权限原则，只授予必要的权限
- 限定在单个命名空间，降低安全风险
- patch 权限用于写回域名注解

---

## 8. 错误处理和重试策略

### 决策
使用 client-go 内置的重试机制 + 指数退避。

### 理由
- Informer 自动处理 Watch 连接中断和重连
- 网络错误、API Server 不可用等临时错误自动恢复
- 减少错误处理代码复杂度

### 错误分类
1. **可恢复错误**：网络超时、API Server 重启 → 自动重试
2. **配置错误**：无效的 kubeconfig、权限不足 → 记录错误并停止
3. **业务逻辑错误**：注解格式错误、端口超范围 → 记录警告并跳过

### 日志记录
使用 Caddy 的日志系统记录所有关键事件：
- INFO：路由创建/删除
- WARN：注解缺失、使用默认值
- ERROR：K8s API 连接失败、权限不足

---

## 9. 并发安全

### 决策
使用 `sync.RWMutex` 保护路由表的并发访问。

### 理由
- Informer 回调和 HTTP handler 在不同 goroutine 中运行
- 读多写少场景，RWMutex 性能优于 Mutex
- Go 标准库，无额外依赖

### 并发场景
1. **Informer 回调**：修改路由表（写操作）
2. **HTTP 请求处理**：查询路由表（读操作）
3. **多个 Deployment 同时创建/删除**：Informer 串行处理事件

---

## 10. 测试策略

### 单元测试
- Mock Kubernetes client 测试 Watcher 逻辑
- Mock HTTP 请求测试 Router Manager
- 测试配置加载和验证
- 测试并发安全（race detector）

### 集成测试
- 使用 kind（Kubernetes in Docker）创建测试集群
- 部署真实 Deployment 验证端到端流程
- 测试 Pod 就绪、Deployment 删除等场景

### 性能测试
- 100 个 Deployment 同时创建的响应时间
- 路由查找性能（1000+ 路由）
- 内存占用（长时间运行）

---

## 总结

所有技术决策均已完成，无剩余未知项。主要技术栈：
- **Caddy v2 模块系统**：实现 HTTP handler 和配置加载
- **Kubernetes client-go Informer**：监听资源变化
- **sync.RWMutex**：并发安全的路由表
- **Strategic Merge Patch**：更新 Deployment 注解

准备进入阶段 1：设计与契约。
