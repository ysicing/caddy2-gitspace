# Caddy v2 Kubernetes Deployment 动态路由扩展

自动为 Kubernetes Deployment 创建 Caddy 反向代理路由的 Caddy v2 模块。

## 功能特性

- ✅ 监听 Kubernetes Deployment 创建/删除事件
- ✅ 自动为单副本 Deployment 创建路由
- ✅ 支持通过注解指定端口
- ✅ Pod IP 变化时自动更新路由
- ✅ Deployment 删除或缩容至 0 时自动移除路由
- ✅ 将生成的域名信息写回 Deployment 注解

## 快速开始

### 1. 编译包含插件的 Caddy

```bash
# 安装 xcaddy
go install github.com/caddyserver/xcaddy/cmd/xcaddy@latest

# 编译
xcaddy build --with github.com/ysicing/caddy2-k8s=.

# 验证插件已加载
./caddy list-modules | grep k8s
```

### 2. 配置 Caddyfile

```caddyfile
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
```

### 3. 部署到 Kubernetes

```bash
# 构建 Docker 镜像
docker build -t your-registry/caddy-k8s:latest .
docker push your-registry/caddy-k8s:latest

# 部署到 Kubernetes
kubectl apply -f deployments/caddy-k8s.yaml

# 部署测试应用
kubectl apply -f deployments/example-deployments.yaml

# 验证路由
kubectl get deployment vscode -o jsonpath='{.metadata.annotations.gitspace\.caddy\.route\.url}'
```

完整的部署指南请参考 [DEPLOY.md](deployments/DEPLOY.md)。

## 配置说明

| 参数 | 必需 | 默认值 | 说明 |
|------|------|--------|------|
| `namespace` | ✅ | - | 监听的 Kubernetes 命名空间 |
| `base_domain` | ✅ | - | 基础域名（如 example.com） |
| `default_port` | ❌ | 8089 | 默认端口（Deployment 缺少端口注解时使用） |
| `kubeconfig` | ❌ | 自动检测 | Kubernetes 配置文件路径 |
| `resync_period` | ❌ | 30s | Informer 重新同步周期 |
| `caddy_admin_url` | ❌ | http://localhost:2019 | Caddy Admin API 地址 |
| `caddy_server_name` | ❌ | srv0 | Caddy Server 名称 |

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
