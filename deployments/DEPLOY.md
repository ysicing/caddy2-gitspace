# Caddy K8s 路由插件部署指南

## 快速开始

### 1. 构建包含插件的 Caddy 镜像

```bash
# 克隆仓库
git clone https://github.com/ysicing/caddy2-k8s.git
cd caddy2-k8s

# 构建 Docker 镜像
docker build -t your-registry/caddy-k8s:latest .

# 推送到镜像仓库
docker push your-registry/caddy-k8s:latest
```

### 2. 配置环境变量

编辑 `deployments/caddy-k8s.yaml`，修改以下配置：

```yaml
env:
  - name: K8S_NAMESPACE
    value: "default"  # 修改为您要监听的命名空间

  - name: BASE_DOMAIN
    value: "example.com"  # 修改为您的域名

  - name: DEFAULT_PORT
    value: "8089"  # 默认端口
```

并更新镜像地址：

```yaml
image: your-registry/caddy-k8s:latest  # 修改为您的镜像地址
```

### 3. 部署到 Kubernetes

```bash
# 应用 RBAC 和 Deployment
kubectl apply -f deployments/caddy-k8s.yaml

# 检查部署状态
kubectl get pods -l app=caddy-k8s
kubectl logs -f deployment/caddy-k8s

# 检查 Service
kubectl get svc caddy-k8s
```

### 4. 部署测试应用

```bash
# 部署示例应用
kubectl apply -f deployments/example-deployments.yaml

# 检查 Deployment
kubectl get deployments

# 查看 Deployment 注解（Caddy 会自动写入路由信息）
kubectl get deployment vscode -o jsonpath='{.metadata.annotations}'
```

### 5. 验证路由

```bash
# 获取 Caddy Service 的 LoadBalancer IP
CADDY_IP=$(kubectl get svc caddy-k8s -o jsonpath='{.status.loadBalancer.ingress[0].ip}')

# 测试路由（使用 Host header）
curl -H "Host: vscode.example.com" http://$CADDY_IP/

# 或者配置本地 /etc/hosts（用于测试）
echo "$CADDY_IP vscode.example.com jupyter.example.com myapp.example.com" | sudo tee -a /etc/hosts

# 然后直接访问
curl http://vscode.example.com/
```

## 工作原理

1. **Deployment 创建**：
   - 用户创建单副本 Deployment（`replicas=1`）
   - 添加注解 `gitspace.caddy.default.port: "8080"`（可选）

2. **Caddy 插件监听**：
   - Watcher 监听 Deployment 事件
   - 检查 Deployment Ready 状态
   - 查询就绪的 Pod IP

3. **路由创建**：
   - 调用 Caddy Admin API 创建路由
   - 格式：`<deployment-name>.<base-domain>` → `<pod-ip>:<port>`
   - 写回注解到 Deployment

4. **路由删除**：
   - Deployment 删除或副本数变为 0 时自动删除路由

## 配置说明

### Caddyfile 参数

| 参数 | 必需 | 默认值 | 说明 |
|------|------|--------|------|
| `namespace` | ✅ | - | 监听的 Kubernetes 命名空间 |
| `base_domain` | ✅ | - | 基础域名（如 example.com） |
| `default_port` | ❌ | 8089 | 默认端口 |
| `kubeconfig` | ❌ | 自动检测 | Kubeconfig 路径 |
| `resync_period` | ❌ | 30s | Informer 重新同步周期 |
| `caddy_admin_url` | ❌ | http://localhost:2019 | Admin API 地址 |
| `caddy_server_name` | ❌ | srv0 | Caddy Server 名称 |

### Deployment 注解

**输入注解**（用户配置）：
- `gitspace.caddy.default.port`: 指定目标端口（可选）

**输出注解**（插件自动写入）：
- `gitspace.caddy.route.url`: 生成的域名
- `gitspace.caddy.route.synced-at`: 路由同步时间
- `gitspace.caddy.route.id`: 路由 ID

### 示例

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: vscode
  annotations:
    gitspace.caddy.default.port: "8080"  # 输入注解
    # 以下注解会被自动写入：
    # gitspace.caddy.route.url: "vscode.example.com"
    # gitspace.caddy.route.synced-at: "2025-01-08T10:30:00Z"
    # gitspace.caddy.route.id: "k8s:default:vscode"
spec:
  replicas: 1
  # ...
```

## 故障排查

### 查看插件日志

```bash
# 查看 Caddy 日志
kubectl logs -f deployment/caddy-k8s

# 查看特定 Deployment 的路由信息
kubectl get deployment vscode -o yaml | grep gitspace.caddy
```

### 常见问题

**1. 路由没有创建**

检查：
- Deployment 的 `replicas` 是否为 1
- Deployment 是否 Ready（`kubectl get deployment`）
- Pod 是否就绪（`kubectl get pods`）

**2. 503 错误**

检查：
- Pod IP 是否正确（`kubectl get pod -o wide`）
- 端口配置是否正确
- Pod 是否监听指定端口

**3. 权限错误**

检查：
- ServiceAccount 是否正确绑定
- ClusterRole 权限是否足够

```bash
kubectl auth can-i get deployments --as=system:serviceaccount:default:caddy-k8s
kubectl auth can-i patch deployments --as=system:serviceaccount:default:caddy-k8s
```

## 卸载

```bash
# 删除示例应用
kubectl delete -f deployments/example-deployments.yaml

# 删除 Caddy
kubectl delete -f deployments/caddy-k8s.yaml
```

## 注意事项

- ⚠️ **仅支持单副本 Deployment**（`replicas=1`）
- ⚠️ **仅监听单个命名空间**
- ⚠️ Pod IP 变化时有短暂无路由窗口（通常 < 1 秒）

## 生产环境建议

1. **使用持久化域名解析**：
   - 配置通配符 DNS：`*.example.com` → Caddy LoadBalancer IP

2. **启用 HTTPS**：
   - 修改 Caddyfile，使用 `:443` 端口
   - Caddy 会自动签发 Let's Encrypt 证书

3. **监控和日志**：
   - 集成 Prometheus metrics
   - 收集 Caddy 日志到中心化日志系统

4. **高可用**：
   - 虽然插件本身建议单副本运行，但可以使用 Kubernetes Deployment 的自动重启
   - LoadBalancer Service 提供稳定的入口
