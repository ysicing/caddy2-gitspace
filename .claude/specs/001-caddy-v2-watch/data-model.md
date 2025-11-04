# 数据模型：Caddy v2 Kubernetes Deployment 动态路由扩展

**日期**：2025-11-04（更新）
**功能**：001-caddy-v2-watch

## 概述

本文档定义了插件中使用的核心数据结构和领域模型。

**架构说明**：本插件不维护路由表，路由由 Caddy 通过 Admin API 管理。插件只维护轻量级的 Route ID 映射用于删除操作。详见 `ARCHITECTURE.md`。

---

## 1. 配置模型

### 1.1 扩展配置 (Config)

插件配置结构，从环境变量或配置文件加载。

**字段**：
- `Namespace` (string, required)：监听的 Kubernetes 命名空间
- `BaseDomain` (string, required)：基础域名，用于生成路由（如 "example.com"）
- `DefaultPort` (int, optional)：默认端口，当 Deployment 缺少端口注解时使用，默认 8089
- `KubeConfig` (string, optional)：Kubernetes 配置文件路径，集群内运行时可为空
- `ResyncPeriod` (duration, optional)：Informer 重新同步周期，默认 30m
- `CaddyAdminURL` (string, optional)：Caddy Admin API 地址，默认 "http://localhost:2019"
- `CaddyServerName` (string, optional)：Caddy 目标 server 名称，默认 "srv0"

**验证规则**：
- `Namespace` 不能为空
- `BaseDomain` 必须是有效的域名格式
- `DefaultPort` 范围 1-65535
- `ResyncPeriod` >= 0
- `CaddyAdminURL` 必须是有效的 HTTP URL

**Go 结构体**：
```go
type Config struct {
    Namespace       string `json:"namespace"`
    BaseDomain      string `json:"base_domain"`
    DefaultPort     int    `json:"default_port,omitempty"`
    KubeConfig      string `json:"kubeconfig,omitempty"`
    ResyncPeriod    string `json:"resync_period,omitempty"`
    CaddyAdminURL   string `json:"caddy_admin_url,omitempty"`
    CaddyServerName string `json:"caddy_server_name,omitempty"`
}
```

---

## 2. Route ID 跟踪模型

### 2.1 Route ID Tracker

轻量级映射，用于跟踪 Deployment 到 Caddy Route ID 的关系。

**用途**：
- 删除路由时快速查找 Route ID
- 不存储路由详情（由 Caddy 管理）

**存储结构**：
```go
type RouteIDTracker struct {
    // deploymentKey (namespace/name) → routeID
    routes map[string]string
    mu     sync.RWMutex
}
```

**操作**：
- `Set(deploymentKey, routeID string)`: 记录映射
- `Get(deploymentKey string) (routeID string, exists bool)`: 查询 Route ID
- `Delete(deploymentKey string)`: 删除映射
- `List() map[string]string`: 列出所有映射（调试用）

**示例**：
```go
tracker := &RouteIDTracker{
    routes: make(map[string]string),
}

// 创建路由后记录
tracker.Set("default/vscode", "k8s-default-vscode")

// 删除路由时查询
routeID, exists := tracker.Get("default/vscode")
if exists {
    // 调用 Caddy Admin API 删除: DELETE /config/.../routes/@id:k8s-default-vscode
    tracker.Delete("default/vscode")
}
```

---

## 3. Caddy Admin API 模型

### 3.1 Caddy 路由配置（由 Caddy 管理）

插件通过 Admin API 管理的路由配置格式。

**JSON 结构**：
```json
{
  "@id": "k8s-default-vscode",
  "match": [
    {
      "host": ["vscode.example.com"]
    }
  ],
  "handle": [
    {
      "handler": "reverse_proxy",
      "upstreams": [
        {
          "dial": "10.0.0.1:8080"
        }
      ]
    }
  ]
}
```

**字段说明**：
- `@id`: Route ID，格式 `k8s-{namespace}-{deployment-name}`
- `match.host`: 匹配的域名列表
- `handle.handler`: 必须为 "reverse_proxy"
- `upstreams.dial`: 目标地址 `{pod-ip}:{port}`

---

## 4. Kubernetes 资源模型

### 4.1 Deployment 注解

**输入注解**（用户配置）：
- `gitspace.caddy.default.port`：目标端口号（字符串，如 "8080"）
  - 可选，缺失时使用 Config.DefaultPort
  - 必须是有效的端口号（1-65535）

**输出注解**（插件写回）：
- `gitspace.caddy.route.url`：生成的完整路由 URL（如 "vscode.example.com"）
- `gitspace.caddy.route.synced-at`：最后同步时间（RFC3339 格式）
- `gitspace.caddy.route.id`：Caddy Route ID（调试用）

**示例**：
```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: vscode
  annotations:
    gitspace.caddy.default.port: "8080"           # 输入
    gitspace.caddy.route.url: "vscode.example.com" # 输出（插件写回）
    gitspace.caddy.route.synced-at: "2025-11-04T10:00:00Z" # 输出
    gitspace.caddy.route.id: "k8s-default-vscode" # 输出
spec:
  replicas: 1
  ...
```

### 4.2 Pod 状态

**关键字段**：
- `Status.PodIP`：Pod IP 地址，用于路由目标
- `Status.Conditions[]`：Pod 状态条件数组
  - `Type == "Ready" && Status == "True"`：Pod 就绪

**Pod 筛选逻辑**：
1. Pod 属于目标 Deployment（通过 OwnerReferences）
2. Pod 状态为 Ready
3. Pod 有有效的 IP 地址

---

## 5. 事件模型

### 5.1 Deployment 事件

**事件类型**：
- `DeploymentAdded`：新 Deployment 创建
- `DeploymentUpdated`：Deployment 更新（包括注解、副本数变化）
- `DeploymentDeleted`：Deployment 删除

**事件处理**：
```
DeploymentAdded:
  1. 读取注解获取端口（或使用默认端口）
  2. 查找属于该 Deployment 的就绪 Pod
  3. 如果 Pod 就绪：
     a. 生成 Route ID
     b. 调用 Caddy Admin API 创建路由
     c. 记录到 RouteIDTracker
     d. 写回域名注解到 Deployment
  4. 如果 Pod 未就绪：等待 Pod 就绪事件

DeploymentUpdated:
  1. 检查副本数是否变为 0
  2. 如果是：调用 Caddy Admin API 删除路由 + 清理 Tracker
  3. 如果不是：检查 Pod 状态变化

DeploymentDeleted:
  1. 从 RouteIDTracker 查找 Route ID
  2. 调用 Caddy Admin API 删除路由
  3. 清理 Tracker
```

### 5.2 Pod 事件

**事件类型**：
- `PodAdded`：新 Pod 创建
- `PodUpdated`：Pod 更新（包括状态变化）
- `PodDeleted`：Pod 删除

**事件处理**：
```
PodAdded/PodUpdated:
  1. 检查 Pod 是否就绪
  2. 查找所属 Deployment
  3. 如果 Deployment 存在且路由未创建：
     a. 生成 Route ID
     b. 调用 Caddy Admin API 创建路由
     c. 记录到 Tracker
     d. 写回注解
  4. 如果 Pod IP 变化：
     a. 调用 Admin API 删除旧路由
     b. 创建新路由（新 Pod IP）
     c. 更新 Tracker

PodDeleted:
  1. 查找所属 Deployment
  2. 如果是唯一 Pod（单副本）：
     a. 调用 Admin API 删除路由
     b. 清理 Tracker
```

---

## 6. 状态转换

### 6.1 路由生命周期

```
[无路由]
    ↓ (Deployment 创建 + Pod 就绪)
[调用 Caddy Admin API 创建路由]
    ↓
[路由活跃]（Caddy 管理）
    ↓ (Pod IP 变化)
[调用 Admin API 更新路由]
    ↓
[路由活跃]
    ↓ (Deployment 删除 OR 副本数 → 0)
[调用 Admin API 删除路由]
    ↓
[无路由]
```

---

## 7. 数据流

### 7.1 路由创建流程

```
Kubernetes API Server
    ↓ (Watch Event: Deployment Created)
Informer Cache
    ↓ (Event Handler)
Watcher Logic
    ↓ (提取 Pod IP + Port + 生成 Route ID)
调用 Caddy Admin API
    ↓ (POST /config/.../routes)
Caddy 添加路由
    ↓
插件写回注解
    ↓ (PATCH Deployment)
Kubernetes API Server

同时：
插件记录 Route ID
    ↓
RouteIDTracker (Memory)
```

### 7.2 HTTP 请求处理流程（完全由 Caddy 处理）

```
Client HTTP Request
    ↓ (Host: vscode.example.com)
Caddy HTTP Server
    ↓ (路由匹配)
Caddy Route (reverse_proxy)
    ↓ (查找 upstream: 10.0.0.1:8080)
Reverse Proxy to Pod
    ↓
Target Pod
```

**注意**：插件不参与 HTTP 请求处理！

---

## 8. 数据持久化

**内存状态**：
- RouteIDTracker：内存中维护，重启后需要重建
  - 重建方法：启动时查询 Caddy Admin API 获取所有 k8s-* 路由

**Kubernetes 状态**：
- Deployment 注解：持久化在 Kubernetes API Server
- Pod IP/状态：由 Kubernetes 管理

**Caddy 状态**：
- 路由配置：持久化在 Caddy 内存中（可选持久化到文件）
- 通过 Admin API 查询：`GET /config/apps/http/servers/srv0/routes`

**恢复机制**：
插件启动时：
1. Informer 启动后执行 ListAndWatch
2. 处理所有现有 Deployment
3. 查询 Caddy Admin API 获取现有路由
4. 重建 RouteIDTracker 映射
5. 时间复杂度：O(N)，N 为 Deployment 数量

---

## 9. 并发控制

**读写场景**：
- **写操作**：Informer 事件回调（单线程，串行处理）
- **Caddy API 调用**：可能并发（但 Caddy 内部处理并发）

**锁策略**：
- `RouteIDTracker` 使用 `sync.RWMutex`
- 写操作（Set/Delete）：`mu.Lock()`
- 读操作（Get）：`mu.RLock()`

**幂等性**：
- 路由创建：使用 `@id`，重复创建不会冲突
- 路由删除：404 不视为错误

---

## 总结

核心数据模型已简化：
- **配置模型**：Config（新增 CaddyAdminURL 和 CaddyServerName）
- **跟踪模型**：RouteIDTracker（轻量级映射）
- **Caddy 模型**：由 Caddy 管理，通过 Admin API 操作
- **Kubernetes 资源**：Deployment 注解、Pod 状态
- **事件模型**：Deployment/Pod 事件 → Caddy Admin API 调用

插件职责清晰：**监听 K8s 事件 → 调用 Caddy Admin API → 维护 Route ID 映射**
