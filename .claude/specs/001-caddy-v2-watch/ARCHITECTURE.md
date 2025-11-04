# 架构澄清：Caddy K8s 路由插件

## 核心原则

**插件职责**：发现（Discover）、创建（Create）、检查（Check）、销毁（Destroy）
**路由管理**：由 Caddy 本身负责，插件通过 Admin API 操作

---

## 正确的架构

### 插件不做什么
❌ 不维护自己的路由表
❌ 不实现 HTTP handler 来处理请求
❌ 不做反向代理逻辑

### 插件做什么
✅ 监听 Kubernetes Deployment 和 Pod 事件（发现）
✅ 调用 Caddy Admin API 添加路由（创建）
✅ 监控 Deployment/Pod 状态变化（检查）
✅ 调用 Caddy Admin API 删除路由（销毁）

---

## 工作流程

```
Kubernetes API
    ↓ (Watch Events)
K8s Watcher (插件)
    ↓ (发现 Deployment 就绪)
调用 Caddy Admin API
    ↓ (POST /config/apps/http/servers/srv0/routes)
Caddy 添加 reverse_proxy 路由
    ↓
HTTP 请求 → Caddy 处理 → 反向代理到 Pod
```

### Deployment 删除流程

```
Kubernetes API
    ↓ (Delete Event)
K8s Watcher (插件)
    ↓ (发现 Deployment 删除)
调用 Caddy Admin API
    ↓ (DELETE /config/apps/http/servers/srv0/routes/@id:xxx)
Caddy 删除路由
```

---

## 核心组件

### 1. K8s Watcher
- 监听 Deployment 和 Pod 事件
- 提取配置信息（端口、域名）
- 触发路由操作

### 2. Caddy Admin API Client
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

### 更新路由（Pod IP 变化）

```bash
# 先删除旧路由
curl -X DELETE http://localhost:2019/config/apps/http/servers/srv0/routes/@id:k8s-default-vscode

# 再添加新路由（新 Pod IP）
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
3. **功能完整**：可以利用 Caddy 的所有特性（健康检查、负载均衡、TLS 等）
4. **配置透明**：可以通过 Admin API 直接查看和修改路由
5. **易于调试**：路由配置独立于插件代码

---

## 部署模式

### 方式 1：Sidecar 模式（推荐）

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: caddy-k8s
spec:
  template:
    spec:
      containers:
      - name: caddy
        image: caddy:latest
        ports:
        - containerPort: 80
        - containerPort: 2019  # Admin API
      - name: k8s-watcher
        image: caddy-k8s-watcher:latest
        env:
        - name: CADDY_ADMIN_URL
          value: "http://localhost:2019"
```

### 方式 2：Caddy 插件模式

编译为 Caddy 模块，作为 Caddy 进程的一部分运行。

---

## 总结

- **插件 = Kubernetes 事件监听器 + Caddy Admin API 客户端**
- **路由管理 = 完全由 Caddy 负责**
- **数据流 = K8s Events → 插件 → Caddy Admin API → Caddy Config**

这个架构更简单、更清晰、更符合"单一职责"原则。
