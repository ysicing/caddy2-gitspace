# Caddy v2 Kubernetes Deployment 动态路由扩展

自动为 Kubernetes Deployment 创建 Caddy 反向代理路由的 Caddy v2 模块。

## 功能特性

- ✅ 监听 Kubernetes Deployment 创建/删除事件
- ✅ 自动为单副本 Deployment 创建路由
- ✅ 支持通过注解指定端口
- ✅ Pod IP 变化时自动更新路由
- ✅ Deployment 删除或缩容至 0 时自动移除路由
- ✅ 将生成的域名信息写回 Deployment 注解
- ✅ 支持 HTTPS 自动证书（阿里云 DNS 验证）
- ✅ 支持阿里云 ACK 和 SLB 集成

## 快速开始

### 本地测试（HTTP）

```bash
# 1. 构建镜像
docker build -t caddy-k8s:local .

# 2. 部署到本地 Kubernetes（minikube/kind/Docker Desktop）
kubectl apply -f deployments/caddy-k8s-local.yaml
kubectl apply -f deployments/example-deployments.yaml

# 3. 访问测试
curl -H "Host: vscode.localhost" http://localhost:30080/
```

详细说明请参考 [LOCAL-DEPLOY.md](deployments/LOCAL-DEPLOY.md)。

### 生产环境（HTTPS + 阿里云 ACK）

```bash
# 1. 创建阿里云 DNS API Secret
kubectl create secret generic alicloud-dns-secret \
  --from-literal=access-key-id=YOUR_KEY \
  --from-literal=access-key-secret=YOUR_SECRET

# 2. 构建推送镜像到 ACR
docker build -t registry.cn-hangzhou.aliyuncs.com/your-ns/caddy-k8s:latest .
docker push registry.cn-hangzhou.aliyuncs.com/your-ns/caddy-k8s:latest

# 3. 修改配置并部署
kubectl apply -f deployments/caddy-k8s.yaml

# 4. 配置 DNS 通配符解析
# *.your-domain.com → SLB IP
```

详细说明请参考：
- 阿里云 ACK：[ACK-DEPLOY.md](deployments/ACK-DEPLOY.md)
- 通用部署：[DEPLOY.md](deployments/DEPLOY.md)

## 配置说明

| 参数 | 必需 | 默认值 | 说明 |
|------|------|--------|------|
| `namespace` | ✅ | - | 监听的 Kubernetes 命名空间 |
| `base_domain` | ✅ | - | 基础域名(如 example.com) |
| `default_port` | ❌ | 8089 | 默认端口(Deployment 缺少端口注解时使用) |
| `label_selector` | ❌ | - | Label 选择器,用于筛选需要管理的 Deployment |
| `kubeconfig` | ❌ | 自动检测 | Kubernetes 配置文件路径 |
| `resync_period` | ❌ | 30s | Informer 重新同步周期 |
| `reconcile_period` | ❌ | 5m | 全量对账周期 |
| `caddy_admin_url` | ❌ | http://localhost:2019 | Caddy Admin API 地址 |
| `caddy_server_name` | ❌ | srv0 | Caddy Server 名称 |

### Label Selector 筛选

默认情况下,插件会监控命名空间下所有单副本 Deployment。通过 `label_selector` 可以精确控制哪些 Deployment 需要自动路由:

```
k8s_router {
    namespace default
    base_domain example.com
    # 只管理带有特定标签的 Deployment
    label_selector "app.kubernetes.io/managed-by=caddy"
}
```

多个 label 使用逗号分隔:
```
label_selector "env=production,team=backend"
```

## Deployment 注解

### 输入注解

- `gitspace.caddy.default.port`: 指定目标端口（可选，默认使用 `default_port`）

示例：
```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: vscode
  annotations:
    gitspace.caddy.default.port: "8080"
spec:
  replicas: 1
  # ...
```

### 输出注解（自动写回）

- `gitspace.caddy.route.url`: 生成的域名（如 `vscode.example.com`）
- `gitspace.caddy.route.synced-at`: 路由同步时间戳
- `gitspace.caddy.route.id`: 路由 ID

## 使用示例

### 创建一个 GitSpace Deployment

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: vscode
  namespace: default
  labels:
    # 可选: 添加标签用于筛选
    app.kubernetes.io/managed-by: caddy
  annotations:
    # 指定应用监听的端口
    gitspace.caddy.default.port: "8080"
spec:
  replicas: 1  # 必须是 1
  selector:
    matchLabels:
      app: vscode
  template:
    metadata:
      labels:
        app: vscode
        app.kubernetes.io/managed-by: caddy
    spec:
      containers:
        - name: vscode
          image: codercom/code-server:latest
          ports:
            - containerPort: 8080
```

部署后，Caddy 会自动：
1. 创建路由：`vscode.example.com` → `<pod-ip>:8080`
2. 写回注解到 Deployment：
   ```yaml
   annotations:
     gitspace.caddy.route.url: "vscode.example.com"
     gitspace.caddy.route.synced-at: "2025-01-08T10:30:00Z"
     gitspace.caddy.route.id: "k8s:default:vscode"
   ```

### 访问应用

```bash
# 获取 Caddy Service IP
CADDY_IP=$(kubectl get svc caddy-k8s -o jsonpath='{.status.loadBalancer.ingress[0].ip}')

# 配置 DNS 或 /etc/hosts
echo "$CADDY_IP vscode.example.com" | sudo tee -a /etc/hosts

# 访问应用
curl http://vscode.example.com/
```

更多示例请参考 [example-deployments.yaml](deployments/example-deployments.yaml)。

## 限制和约束

- ⚠️ **仅支持单副本 Deployment**（`replicas=1`）
- ⚠️ **仅监听单个命名空间**
- ⚠️ Pod IP 变化时有短暂的无路由窗口（< 1 秒）

## 架构说明

详细的架构设计请参考 [ARCHITECTURE.md](.claude/specs/001-caddy-v2-watch/ARCHITECTURE.md)。

核心工作流程：
```
K8s Event → Watcher → EventHandler → AdminAPIClient → Caddy Admin API → 路由创建/删除
```

## 开发

```bash
# 编译所有包
go build ./...

# 运行 vet 检查
go vet ./...

# 查看项目结构
tree -L 2
```

## 许可证

MIT License
