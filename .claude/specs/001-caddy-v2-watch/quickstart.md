# 快速开始：Caddy v2 Kubernetes Deployment 动态路由扩展

**日期**：2025-11-04
**功能**：001-caddy-v2-watch

本文档提供从零开始部署和测试 Caddy v2 K8s 动态路由扩展的完整指南。

---

## 前置条件

1. **Kubernetes 集群**：
   - Kubernetes 1.20+
   - kubectl 已配置并可访问集群
   - 具备集群管理员权限（用于创建 RBAC 资源）

2. **开发环境**：
   - Go 1.25.3+
   - Docker（用于构建镜像）
   - kind 或 minikube（可选，用于本地测试）

3. **域名配置**：
   - 一个可用的域名（如 `*.example.com`）
   - DNS 配置指向 Caddy 服务的入口 IP

---

## 步骤 1：创建 Kubernetes RBAC 资源

创建 ServiceAccount、Role 和 RoleBinding，授予 Caddy 扩展必要的权限。

```bash
kubectl apply -f - <<EOF
apiVersion: v1
kind: ServiceAccount
metadata:
  name: caddy-k8s-router
  namespace: default
---
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: caddy-k8s-router
  namespace: default
rules:
- apiGroups: ["apps"]
  resources: ["deployments"]
  verbs: ["get", "list", "watch", "patch"]
- apiGroups: [""]
  resources: ["pods"]
  verbs: ["get", "list", "watch"]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: caddy-k8s-router
  namespace: default
subjects:
- kind: ServiceAccount
  name: caddy-k8s-router
  namespace: default
roleRef:
  kind: Role
  name: caddy-k8s-router
  apiGroup: rbac.authorization.k8s.io
EOF
```

**验证**：
```bash
kubectl get sa caddy-k8s-router -n default
kubectl get role caddy-k8s-router -n default
kubectl get rolebinding caddy-k8s-router -n default
```

---

## 步骤 2：使用 xcaddy 编译 Caddy（含 K8s 插件）

### 2.1 安装 xcaddy

xcaddy 是 Caddy 的自定义构建工具，用于编译包含第三方模块的 Caddy。

```bash
go install github.com/caddyserver/xcaddy/cmd/xcaddy@latest
```

### 2.2 编译包含 K8s 插件的 Caddy

```bash
# 在项目根目录下执行
xcaddy build \
  --with github.com/ysicing/caddy2-gitspace=.
```

这将生成一个名为 `caddy` 的可执行文件，其中包含了 K8s 路由插件。

### 2.3 验证插件已加载

```bash
./caddy list-modules | grep k8s
```

预期输出：
```
http.handlers.k8s_router
```

### 2.4 构建 Docker 镜像

```bash
docker build -t caddy-k8s-router:latest -f Dockerfile .
```

**Dockerfile 示例**：
```dockerfile
# 第一阶段：编译 Caddy（含 K8s 插件）
FROM golang:1.25.3-alpine AS builder
WORKDIR /app

# 安装 xcaddy
RUN go install github.com/caddyserver/xcaddy/cmd/xcaddy@latest

# 复制源代码
COPY . .

# 使用 xcaddy 编译 Caddy（包含本地模块）
RUN xcaddy build --with github.com/ysicing/caddy2-gitspace=.

# 第二阶段：运行时镜像
FROM alpine:latest
RUN apk --no-cache add ca-certificates
WORKDIR /root/

# 复制编译好的 Caddy
COPY --from=builder /app/caddy .

EXPOSE 80 443 2019
CMD ["./caddy", "run", "--config", "/etc/caddy/Caddyfile"]
```

---

## 步骤 3：部署 Caddy 到 Kubernetes

### 3.1 创建 Caddyfile ConfigMap

```bash
kubectl create configmap caddy-config \
  --from-literal=Caddyfile="$(cat <<'EOF'
{
    admin localhost:2019
    persist_config off
}

:80 {
    k8s_router {
        namespace default
        base_domain example.com
        default_port 8089
    }
}
EOF
)" \
  -n default
```

**Caddyfile 解释**：
- `admin localhost:2019`：启用 Admin API（仅容器内访问）
- `persist_config off`：关闭配置持久化（插件会动态管理路由）
- `k8s_router`：启用 K8s 路由插件
  - `namespace default`：监听的 K8s 命名空间
  - `base_domain example.com`：路由域名后缀
  - `default_port 8089`：默认端口（当 Deployment 缺少端口注解时使用）

**注意**：`k8s_router` 指令会：
1. 在 Caddy 启动时初始化插件
2. 启动 Kubernetes Informer 监听 Deployment/Pod 事件
3. 通过 Admin API 动态添加/删除路由（不在 Caddyfile 中显示）

### 3.2 创建 Deployment

```bash
kubectl apply -f - <<EOF
apiVersion: apps/v1
kind: Deployment
metadata:
  name: caddy-k8s-router
  namespace: default
spec:
  replicas: 1
  selector:
    matchLabels:
      app: caddy-k8s-router
  template:
    metadata:
      labels:
        app: caddy-k8s-router
    spec:
      serviceAccountName: caddy-k8s-router
      containers:
      - name: caddy
        image: caddy-k8s-router:latest
        imagePullPolicy: IfNotPresent
        ports:
        - containerPort: 80
          name: http
        volumeMounts:
        - name: config
          mountPath: /etc/caddy
          readOnly: true
      volumes:
      - name: config
        configMap:
          name: caddy-config
EOF
```

### 3.3 创建 Service

```bash
kubectl apply -f - <<EOF
apiVersion: v1
kind: Service
metadata:
  name: caddy-k8s-router
  namespace: default
spec:
  type: LoadBalancer
  selector:
    app: caddy-k8s-router
  ports:
  - port: 80
    targetPort: 80
    name: http
EOF
```

**验证**：
```bash
kubectl get pods -n default -l app=caddy-k8s-router
kubectl logs -n default -l app=caddy-k8s-router
```

---

## 步骤 4：部署测试应用

### 4.1 创建测试 Deployment

```bash
kubectl apply -f - <<EOF
apiVersion: apps/v1
kind: Deployment
metadata:
  name: vscode
  namespace: default
  annotations:
    gitspace.caddy.default.port: "8080"
spec:
  replicas: 1
  selector:
    matchLabels:
      app: vscode
  template:
    metadata:
      labels:
        app: vscode
    spec:
      containers:
      - name: vscode
        image: codercom/code-server:latest
        ports:
        - containerPort: 8080
        env:
        - name: PASSWORD
          value: "testpassword"
EOF
```

### 4.2 等待 Pod 就绪

```bash
kubectl wait --for=condition=ready pod -l app=vscode -n default --timeout=60s
```

### 4.3 检查路由注解

```bash
kubectl get deployment vscode -n default -o jsonpath='{.metadata.annotations.gitspace\.caddy\.route\.url}'
```

**预期输出**：
```
vscode.example.com
```

---

## 步骤 5：测试路由

### 5.1 获取 Caddy Service 外部 IP

```bash
CADDY_IP=$(kubectl get svc caddy-k8s-router -n default -o jsonpath='{.status.loadBalancer.ingress[0].ip}')
echo "Caddy IP: $CADDY_IP"
```

### 5.2 测试 HTTP 请求

```bash
curl -H "Host: vscode.example.com" http://$CADDY_IP/
```

**预期结果**：
- 返回 VS Code Server 登录页面
- HTTP 状态码 200

### 5.3 配置本地 DNS（可选）

编辑 `/etc/hosts`：
```
<CADDY_IP> vscode.example.com
```

然后在浏览器访问：
```
http://vscode.example.com
```

---

## 步骤 6：验收测试场景

### 场景 1：Deployment 创建后自动生成路由

**操作**：
```bash
kubectl apply -f - <<EOF
apiVersion: apps/v1
kind: Deployment
metadata:
  name: jupyter
  namespace: default
  annotations:
    gitspace.caddy.default.port: "8888"
spec:
  replicas: 1
  selector:
    matchLabels:
      app: jupyter
  template:
    metadata:
      labels:
        app: jupyter
    spec:
      containers:
      - name: jupyter
        image: jupyter/base-notebook:latest
        ports:
        - containerPort: 8888
EOF
```

**验证**：
```bash
# 等待 Pod 就绪
kubectl wait --for=condition=ready pod -l app=jupyter -n default --timeout=60s

# 检查注解
kubectl get deployment jupyter -n default -o yaml | grep gitspace.caddy.route.url

# 测试路由
curl -H "Host: jupyter.example.com" http://$CADDY_IP/
```

**预期**：路由正常工作。

---

### 场景 2：Deployment 删除后自动移除路由

**操作**：
```bash
kubectl delete deployment jupyter -n default
```

**验证**：
```bash
# 等待 1 秒
sleep 1

# 测试路由（应该失败）
curl -H "Host: jupyter.example.com" http://$CADDY_IP/
```

**预期**：返回 404 或被 next handler 处理。

---

### 场景 3：缺少端口注解时使用默认端口

**操作**：
```bash
kubectl apply -f - <<EOF
apiVersion: apps/v1
kind: Deployment
metadata:
  name: nginx
  namespace: default
  # 注意：没有 gitspace.caddy.default.port 注解
spec:
  replicas: 1
  selector:
    matchLabels:
      app: nginx
  template:
    metadata:
      labels:
        app: nginx
    spec:
      containers:
      - name: nginx
        image: nginx:alpine
        ports:
        - containerPort: 8089  # 使用默认端口
EOF
```

**验证**：
```bash
kubectl wait --for=condition=ready pod -l app=nginx -n default --timeout=60s
curl -H "Host: nginx.example.com" http://$CADDY_IP/
```

**预期**：路由到端口 8089。

---

### 场景 4：Pod 重启后路由自动更新

**操作**：
```bash
# 删除 vscode Pod（模拟 Pod 重启）
kubectl delete pod -l app=vscode -n default

# 等待新 Pod 就绪
kubectl wait --for=condition=ready pod -l app=vscode -n default --timeout=60s
```

**验证**：
```bash
# 路由应该仍然工作（指向新 Pod IP）
curl -H "Host: vscode.example.com" http://$CADDY_IP/
```

**预期**：路由正常工作，Pod IP 已更新。

---

### 场景 5：Deployment 副本数缩减至 0

**操作**：
```bash
kubectl scale deployment vscode --replicas=0 -n default
```

**验证**：
```bash
# 等待 1 秒
sleep 1

# 测试路由（应该失败）
curl -H "Host: vscode.example.com" http://$CADDY_IP/
```

**预期**：路由已被移除。

---

## 步骤 7：查看日志和监控

### 7.1 查看 Caddy 日志

```bash
kubectl logs -f -n default -l app=caddy-k8s-router
```

**关键日志**：
- `INFO: Route added: vscode.example.com -> 10.0.0.1:8080`
- `INFO: Route deleted: jupyter.example.com`
- `WARN: Deployment 'nginx' missing port annotation, using default: 8089`

### 7.2 查看路由表（调试接口）

如果实现了调试接口：
```bash
kubectl exec -n default deployment/caddy-k8s-router -- curl localhost:2019/routes
```

**预期输出**（JSON）：
```json
[
  {
    "deployment_key": "default/vscode",
    "domain": "vscode.example.com",
    "target_ip": "10.0.0.1",
    "target_port": 8080,
    "created_at": "2025-11-04T10:00:00Z"
  }
]
```

---

## 步骤 8：清理资源

```bash
# 删除测试 Deployments
kubectl delete deployment vscode nginx -n default

# 删除 Caddy 资源
kubectl delete deployment,svc,configmap -l app=caddy-k8s-router -n default

# 删除 RBAC 资源
kubectl delete sa,role,rolebinding caddy-k8s-router -n default
```

---

## 常见问题

### 问题 1：路由未创建

**症状**：Deployment 创建后，注解中没有 `gitspace.caddy.route.url`。

**排查**：
1. 检查 Pod 是否就绪：`kubectl get pods -n default`
2. 检查 Caddy 日志：`kubectl logs -n default -l app=caddy-k8s-router`
3. 检查 RBAC 权限：`kubectl auth can-i patch deployments --as=system:serviceaccount:default:caddy-k8s-router -n default`

**解决**：
- 等待 Pod 就绪
- 修复 RBAC 权限

---

### 问题 2：路由无法访问

**症状**：`curl` 返回连接拒绝或超时。

**排查**：
1. 检查路由是否存在：查看 Caddy 日志
2. 检查 Pod IP：`kubectl get pod -o wide -n default`
3. 测试 Pod 直接访问：`kubectl exec -it <caddy-pod> -- curl <pod-ip>:<port>`

**解决**：
- 确认网络策略允许 Caddy Pod 访问目标 Pod
- 检查目标 Pod 端口配置

---

### 问题 3：Caddy 启动失败

**症状**：Caddy Pod 一直处于 CrashLoopBackOff。

**排查**：
```bash
kubectl describe pod -n default -l app=caddy-k8s-router
kubectl logs -n default -l app=caddy-k8s-router --previous
```

**常见原因**：
- 配置文件格式错误（JSON 语法）
- 缺少 RBAC 权限
- Kubeconfig 路径错误

---

## 总结

通过以上步骤，您已经：
1. ✅ 部署了 Caddy K8s 动态路由扩展
2. ✅ 验证了所有核心功能（路由创建、更新、删除）
3. ✅ 测试了边缘情况（缺少注解、Pod 重启、副本缩减）
4. ✅ 了解了如何排查常见问题

**下一步**：
- 配置生产环境的 DNS 和 TLS
- 启用 Prometheus 监控
- 扩展到多命名空间支持（可选）
