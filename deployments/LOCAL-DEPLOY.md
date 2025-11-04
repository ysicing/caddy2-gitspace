# 本地测试部署指南

本指南用于在本地 Kubernetes 集群（如 minikube、kind、Docker Desktop）中测试 Caddy K8s 路由插件。

## 前提条件

选择以下任一本地 Kubernetes 环境：

### 选项 1: Docker Desktop（推荐，macOS/Windows）

```bash
# 启用 Kubernetes
# Docker Desktop → Settings → Kubernetes → Enable Kubernetes

# 验证
kubectl cluster-info
```

### 选项 2: minikube（Linux/macOS/Windows）

```bash
# 安装 minikube
brew install minikube  # macOS
# 或参考：https://minikube.sigs.k8s.io/docs/start/

# 启动集群
minikube start --driver=docker

# 验证
kubectl get nodes
```

### 选项 3: kind（Kubernetes in Docker）

```bash
# 安装 kind
brew install kind  # macOS
# 或参考：https://kind.sigs.k8s.io/docs/user/quick-start/

# 创建集群
kind create cluster --name caddy-test

# 验证
kubectl cluster-info --context kind-caddy-test
```

## 快速开始

### 1. 构建本地镜像

```bash
# 进入项目目录
cd caddy2-k8s

# 构建镜像（本地测试不需要推送到镜像仓库）
docker build -t caddy-k8s:local .

# 验证镜像
docker images | grep caddy-k8s

# 如果使用 minikube，需要加载镜像
# minikube image load caddy-k8s:local

# 如果使用 kind，需要加载镜像
# kind load docker-image caddy-k8s:local --name caddy-test
```

### 2. 部署 RBAC（使用生产环境的配置）

```bash
# 创建 ServiceAccount 和 RBAC
kubectl apply -f - <<EOF
---
apiVersion: v1
kind: ServiceAccount
metadata:
  name: caddy-k8s
  namespace: default

---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: caddy-k8s-reader
rules:
  - apiGroups: ["apps"]
    resources: ["deployments"]
    verbs: ["get", "list", "watch", "patch"]
  - apiGroups: [""]
    resources: ["pods"]
    verbs: ["get", "list", "watch"]

---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: caddy-k8s-reader-binding
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: caddy-k8s-reader
subjects:
  - kind: ServiceAccount
    name: caddy-k8s
    namespace: default
EOF
```

### 3. 部署 Caddy（本地测试版）

```bash
# 部署 Caddy K8s Router
kubectl apply -f deployments/caddy-k8s-local.yaml

# 检查部署状态
kubectl get pods -l app=caddy-k8s-local
kubectl logs -f deployment/caddy-k8s-local

# 检查 Service
kubectl get svc caddy-k8s-local
```

### 4. 部署测试应用

```bash
# 部署示例应用
kubectl apply -f deployments/example-deployments.yaml

# 等待 Pod 就绪
kubectl wait --for=condition=available --timeout=60s deployment/vscode

# 查看路由注解
kubectl get deployment vscode -o jsonpath='{.metadata.annotations}' | jq
```

### 5. 访问测试

#### 方法 1: 使用 NodePort（推荐）

```bash
# 获取 NodePort（应该是 30080）
kubectl get svc caddy-k8s-local

# 测试访问
curl -H "Host: vscode.localhost" http://localhost:30080/
curl -H "Host: jupyter.localhost" http://localhost:30080/
curl -H "Host: myapp.localhost" http://localhost:30080/

# 或者配置 /etc/hosts
echo "127.0.0.1 vscode.localhost jupyter.localhost myapp.localhost" | sudo tee -a /etc/hosts

# 直接访问
curl http://vscode.localhost:30080/
```

#### 方法 2: 使用 kubectl port-forward

```bash
# 端口转发
kubectl port-forward svc/caddy-k8s-local 8080:80

# 在另一个终端测试
curl -H "Host: vscode.localhost" http://localhost:8080/
```

#### 方法 3: 使用 minikube service（仅 minikube）

```bash
# minikube 会自动打开浏览器
minikube service caddy-k8s-local
```

### 6. 验证路由

```bash
# 检查 Caddy 日志
kubectl logs -f deployment/caddy-k8s-local | grep -i "route"

# 查看所有 Deployment 的路由注解
kubectl get deployments -o json | jq '.items[] | {name: .metadata.name, annotations: .metadata.annotations}'

# 测试健康检查
curl http://localhost:30080/healthz
```

## 本地开发调试

### 使用本地 Caddyfile 运行

```bash
# 编译 Caddy（包含插件）
xcaddy build --with github.com/ysicing/caddy2-gitspace=.

# 使用本地 Caddyfile 运行（需要在集群外配置 kubeconfig）
export KUBECONFIG=~/.kube/config
export K8S_NAMESPACE=default
export BASE_DOMAIN=localhost
export DEFAULT_PORT=8089

./caddy run --config Caddyfile.local

# 测试
curl -H "Host: vscode.localhost" http://localhost/
```

### 实时查看日志

```bash
# 查看 Caddy 日志
kubectl logs -f deployment/caddy-k8s-local

# 查看事件
kubectl get events --watch

# 查看 Deployment 状态
watch kubectl get deployments
```

### 调试技巧

```bash
# 进入 Caddy 容器
kubectl exec -it deployment/caddy-k8s-local -- sh

# 查看 Caddy Admin API
kubectl exec -it deployment/caddy-k8s-local -- curl http://localhost:2019/config/

# 查看当前路由
kubectl exec -it deployment/caddy-k8s-local -- curl http://localhost:2019/config/apps/http/servers/srv0/routes | jq
```

## 测试场景

### 场景 1: Deployment 创建

```bash
# 创建新 Deployment
kubectl apply -f - <<EOF
apiVersion: apps/v1
kind: Deployment
metadata:
  name: test-app
  annotations:
    gitspace.caddy.default.port: "8080"
spec:
  replicas: 1
  selector:
    matchLabels:
      app: test-app
  template:
    metadata:
      labels:
        app: test-app
    spec:
      containers:
        - name: nginx
          image: nginx:alpine
          ports:
            - containerPort: 8080
EOF

# 等待并检查路由
sleep 5
kubectl get deployment test-app -o jsonpath='{.metadata.annotations}' | jq

# 测试访问
curl -H "Host: test-app.localhost" http://localhost:30080/
```

### 场景 2: Pod 删除和重建

```bash
# 删除 Pod（Deployment 会重建）
kubectl delete pod -l app=vscode

# 观察日志（应该看到路由删除和重建）
kubectl logs -f deployment/caddy-k8s-local | grep -i "vscode"

# 测试访问（稍等片刻后应该恢复）
curl -H "Host: vscode.localhost" http://localhost:30080/
```

### 场景 3: Deployment 扩缩容

```bash
# 缩容到 0（路由应该被删除）
kubectl scale deployment vscode --replicas=0

# 检查注解（路由信息应该被移除）
kubectl get deployment vscode -o jsonpath='{.metadata.annotations}' | jq

# 扩容回 1（路由应该重新创建）
kubectl scale deployment vscode --replicas=1

# 等待并检查
sleep 5
kubectl get deployment vscode -o jsonpath='{.metadata.annotations}' | jq
```

### 场景 4: Deployment 删除

```bash
# 删除 Deployment
kubectl delete deployment test-app

# 检查路由是否被删除（应该看不到 test-app 相关路由）
kubectl exec -it deployment/caddy-k8s-local -- \
  curl -s http://localhost:2019/config/apps/http/servers/srv0/routes | \
  jq '.[] | select(."@id" | contains("test-app"))'
```

## 常见问题

### 1. 镜像拉取失败

```bash
# 确保镜像已构建
docker images | grep caddy-k8s

# 如果使用 minikube
minikube image load caddy-k8s:local

# 如果使用 kind
kind load docker-image caddy-k8s:local --name caddy-test
```

### 2. Pod 无法启动

```bash
# 查看 Pod 详情
kubectl describe pod -l app=caddy-k8s-local

# 查看日志
kubectl logs -l app=caddy-k8s-local

# 常见原因：
# - RBAC 权限不足
# - ConfigMap 未创建
# - 镜像不存在
```

### 3. 路由无法访问

```bash
# 检查 Service
kubectl get svc caddy-k8s-local

# 检查路由是否创建
kubectl logs -f deployment/caddy-k8s-local | grep "Route created"

# 检查 Pod IP
kubectl get pods -o wide

# 测试 Pod 是否可访问
kubectl exec -it deployment/caddy-k8s-local -- \
  curl http://<pod-ip>:8080/
```

### 4. DNS 解析问题

```bash
# 本地测试使用 Host header
curl -H "Host: vscode.localhost" http://localhost:30080/

# 或配置 /etc/hosts
echo "127.0.0.1 vscode.localhost" | sudo tee -a /etc/hosts
```

## 清理环境

```bash
# 删除测试应用
kubectl delete -f deployments/example-deployments.yaml

# 删除 Caddy
kubectl delete -f deployments/caddy-k8s-local.yaml

# 删除 RBAC
kubectl delete clusterrolebinding caddy-k8s-reader-binding
kubectl delete clusterrole caddy-k8s-reader
kubectl delete serviceaccount caddy-k8s

# 如果使用 minikube
minikube stop
minikube delete

# 如果使用 kind
kind delete cluster --name caddy-test
```

## 性能测试

```bash
# 使用 Apache Bench 测试
ab -n 1000 -c 10 -H "Host: vscode.localhost" http://localhost:30080/

# 使用 wrk 测试
wrk -t4 -c100 -d30s -H "Host: vscode.localhost" http://localhost:30080/
```

## 下一步

本地测试通过后，可以：
1. 部署到生产环境：参考 [DEPLOY.md](DEPLOY.md)
2. 部署到阿里云 ACK：参考 [ACK-DEPLOY.md](ACK-DEPLOY.md)
3. 启用 HTTPS：修改 Caddyfile 添加 TLS 配置

## 参考资料

- [Docker Desktop Kubernetes](https://docs.docker.com/desktop/kubernetes/)
- [minikube 文档](https://minikube.sigs.k8s.io/docs/)
- [kind 文档](https://kind.sigs.k8s.io/)
- [Caddy 文档](https://caddyserver.com/docs/)
