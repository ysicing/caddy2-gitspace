# 架构设计：Caddy K8s 路由插件

## 核心原则

**部署模式**：作为 Caddy 模块编译和运行
**插件职责**：监听（Watch）→ 发现（Discover）→ 创建（Create）→ 销毁（Destroy）
**路由管理**：通过 Caddy Admin API 操作，由 Caddy 本身负责 HTTP 流量处理
**副本限制**：仅支持单副本 Deployment（replicas = 1）

---

## 正确的架构

### 插件不做什么
❌ 不维护自己的路由表
❌ 不实现 HTTP handler 来直接处理用户请求
❌ 不做反向代理逻辑
❌ 不支持多副本负载均衡

### 插件做什么
✅ 作为 Caddy 模块编译进 Caddy 二进制文件
✅ 监听 Kubernetes Deployment 和 Pod 事件（发现）
✅ 调用 Caddy Admin API 添加路由（创建）
✅ 监控 Deployment/Pod 状态变化（检查）
✅ 调用 Caddy Admin API 删除路由（销毁）
✅ 只处理单副本 Deployment

---

## 工作流程

### 路由创建流程

```
Kubernetes API
    ↓ (Watch Event: Deployment Created, replicas=1)
K8s Watcher (Caddy 模块)
    ↓ (发现 Pod 就绪)
调用 Caddy Admin API (内部调用)
    ↓ (POST /config/apps/http/servers/srv0/routes)
Caddy 添加 reverse_proxy 路由
    ↓
HTTP 请求 → Caddy 处理 → 反向代理到 Pod
```

### Deployment 删除流程

```
Kubernetes API
    ↓ (Delete Event OR replicas=0)
K8s Watcher (Caddy 模块)
    ↓ (发现 Deployment 删除/缩容)
调用 Caddy Admin API
    ↓ (DELETE /config/apps/http/servers/srv0/routes/@id:xxx)
Caddy 删除路由
```

### Pod IP 变化处理（重要）

**策略：删除 + 重建**

```
Pod 删除事件
    ↓
调用 Admin API 删除旧路由
    ↓
新 Pod 就绪事件
    ↓
调用 Admin API 创建新路由（新 Pod IP）
```

**权衡**：
- ✅ 实现简单，逻辑清晰
- ✅ 避免无效路由（指向已删除的 Pod）
- ⚠️ 有短暂无路由窗口（通常 < 1 秒）
- ⚠️ 该窗口期间的请求会 404

**不采用 PATCH 更新的原因**：
- Admin API 对单个路由的就地更新支持有限
- DELETE + CREATE 更可靠，幂等性更好
- 单副本场景下，Pod 重启本身就有服务中断

---

## 核心组件

### 1. K8s Watcher (Caddy 模块)
- 监听 Deployment 和 Pod 事件
- 提取配置信息（端口、域名）
- 触发路由操作
- **过滤条件**：只处理 `spec.replicas == 1` 的 Deployment

### 2. Caddy Admin API Client（内部）
- 封装 Caddy Admin API 调用
- 添加路由：`POST /config/.../routes`
- 删除路由：`DELETE /config/.../routes/@id:xxx`
- 查询路由（可选）：`GET /config/.../routes`

### 3. Route ID Tracker（轻量级映射）
- 只维护 `Deployment Key → Route ID` 的映射
- 用途：删除路由时快速查找 Route ID
- 不包含路由详情（由 Caddy 管理）

---

## Caddy Admin API 使用

### 添加路由示例

```bash
curl -X POST http://localhost:2019/config/apps/http/servers/srv0/routes \
  -H "Content-Type: application/json" \
  -d '{
    "@id": "k8s-default-vscode",
    "match": [{"host": ["vscode.example.com"]}],
    "handle": [{
      "handler": "reverse_proxy",
      "upstreams": [{"dial": "10.0.0.1:8080"}]
    }]
  }'
```

### 删除路由示例

```bash
curl -X DELETE http://localhost:2019/config/apps/http/servers/srv0/routes/@id:k8s-default-vscode
```

### Pod IP 变化处理示例

```bash
# 1. Pod 删除 → 删除路由
curl -X DELETE http://localhost:2019/config/apps/http/servers/srv0/routes/@id:k8s-default-vscode

# 2. 新 Pod 就绪 → 创建新路由（新 Pod IP）
curl -X POST http://localhost:2019/config/apps/http/servers/srv0/routes \
  -H "Content-Type: application/json" \
  -d '{
    "@id": "k8s-default-vscode",
    "match": [{"host": ["vscode.example.com"]}],
    "handle": [{
      "handler": "reverse_proxy",
      "upstreams": [{"dial": "10.0.0.2:8080"}]
    }]
  }'
```

---

## 数据模型简化

### 插件只需维护

```go
type RouteIDTracker struct {
    // deploymentKey (namespace/name) → routeID
    routes map[string]string
    mu     sync.RWMutex
}
```

### Caddy 管理的完整路由

```json
{
  "@id": "k8s-default-vscode",
  "match": [{"host": ["vscode.example.com"]}],
  "handle": [{
    "handler": "reverse_proxy",
    "upstreams": [{"dial": "10.0.0.1:8080"}],
    "health_checks": {...}
  }]
}
```

---

## 优势

1. **职责单一**：插件只做 K8s 事件处理，不涉及 HTTP 流量
2. **性能更好**：Caddy 的反向代理经过高度优化
3. **功能完整**：可以利用 Caddy 的所有特性（健康检查、TLS 等）
4. **配置透明**：可以通过 Admin API 直接查看和修改路由
5. **易于调试**：路由配置独立于插件代码
6. **内置集成**：作为 Caddy 模块运行，无需外部进程管理

---

## 单副本限制说明

### 为什么只支持单副本？

1. **简化设计**：一个 Deployment 对应一个 Pod IP，映射关系清晰
2. **符合场景**：个人开发环境、GitSpace 类应用通常是单副本
3. **避免复杂性**：多副本需要：
   - 监听 EndpointSlice 或 Endpoints
   - 管理多个 upstream
   - 处理部分 Pod 就绪的情况
   - 实现负载均衡策略选择

### 多副本场景的推荐方案

如果需要多副本负载均衡，建议：
1. **使用 Kubernetes Service**：插件路由到 ClusterIP Service，而非直接到 Pod
2. **使用 Ingress Controller**：Nginx Ingress、Traefik 等更适合生产环境
3. **扩展本插件**：未来可以添加 `gitspace.caddy.mode: service` 注解支持

---

## 部署模式

### Caddy 模块编译部署（推荐）

```bash
# 1. 使用 xcaddy 编译带插件的 Caddy
xcaddy build \
  --with github.com/ysicing/caddy2-k8s

# 2. 部署到 Kubernetes
kubectl apply -f deployments/
```

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: caddy-k8s
spec:
  replicas: 1  # Caddy 本身可以多副本
  template:
    spec:
      serviceAccountName: caddy-k8s
      containers:
      - name: caddy
        image: custom-caddy:latest  # 包含 k8s 插件的 Caddy
        ports:
        - containerPort: 80
        - containerPort: 2019  # Admin API（内部访问）
        volumeMounts:
        - name: config
          mountPath: /etc/caddy
      volumes:
      - name: config
        configMap:
          name: caddy-config
```

### Caddyfile 配置示例

```caddyfile
{
    admin localhost:2019
    persist_config off  # 插件会自动管理路由，无需持久化
}

:80 {
    k8s_router {
        namespace default
        base_domain example.com
        default_port 8089
    }
}
```

**注意**：`k8s_router` 是 Caddy 指令，在启动时会：
1. 初始化 Kubernetes client
2. 启动 Informer 监听 Deployment/Pod 事件
3. 通过 Admin API 动态管理路由（不在 Caddyfile 中）

---

## 总结

- **部署模式** = Caddy 模块（编译进 Caddy）
- **插件职责** = Kubernetes 事件监听器 + Caddy Admin API 客户端
- **路由管理** = 完全由 Caddy 负责
- **数据流** = K8s Events → 插件（Caddy 模块）→ Admin API → Caddy Config
- **副本限制** = 仅支持单副本 Deployment
- **Pod IP 变化** = 删除旧路由 + 创建新路由

这个架构更简单、更清晰、更符合"单一职责"原则，同时作为 Caddy 模块运行，部署和管理更方便。
