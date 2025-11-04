# 阿里云 ACK 部署指南

本指南专门针对阿里云容器服务 Kubernetes 版（ACK）的部署说明。

## 前提条件

1. **阿里云 ACK 集群**
   - 已创建 ACK 集群
   - 已配置 kubectl 访问集群

2. **阿里云 DNS**
   - 已有域名并托管在阿里云 DNS
   - 已创建 RAM 用户并授予 DNS 管理权限

3. **阿里云 RAM 权限**
   - 创建 RAM 用户用于 DNS API 访问
   - 授予权限：`AliyunDNSFullAccess`

## 快速部署

### 1. 创建阿里云 DNS API 凭证 Secret

```bash
# 使用您的阿里云 AccessKey 创建 Secret
kubectl create secret generic alicloud-dns-secret \
  --from-literal=access-key-id=YOUR_ACCESS_KEY_ID \
  --from-literal=access-key-secret=YOUR_ACCESS_KEY_SECRET \
  -n default
```

### 2. 构建并推送镜像到阿里云容器镜像服务

```bash
# 登录阿里云容器镜像服务（ACR）
docker login --username=YOUR_ALIYUN_USERNAME registry.cn-hangzhou.aliyuncs.com

# 构建镜像
docker build -t registry.cn-hangzhou.aliyuncs.com/your-namespace/caddy-k8s:latest .

# 推送镜像
docker push registry.cn-hangzhou.aliyuncs.com/your-namespace/caddy-k8s:latest
```

### 3. 修改部署配置

编辑 `deployments/caddy-k8s.yaml`：

```yaml
# 修改镜像地址
image: registry.cn-hangzhou.aliyuncs.com/your-namespace/caddy-k8s:latest

# 修改环境变量
env:
  - name: BASE_DOMAIN
    value: "your-domain.com"  # 修改为您的域名

  - name: K8S_NAMESPACE
    value: "default"  # 修改为您要监听的命名空间
```

### 4. 部署到 ACK

```bash
# 应用部署清单
kubectl apply -f deployments/caddy-k8s.yaml

# 检查部署状态
kubectl get pods -l app=caddy-k8s
kubectl logs -f deployment/caddy-k8s

# 检查 SLB 创建状态
kubectl get svc caddy-k8s
```

### 5. 配置 DNS 解析

```bash
# 获取 SLB 公网 IP
SLB_IP=$(kubectl get svc caddy-k8s -o jsonpath='{.status.loadBalancer.ingress[0].ip}')
echo "SLB IP: $SLB_IP"

# 在阿里云 DNS 控制台添加以下解析记录：
# 记录类型: A
# 主机记录: *.your-domain.com（通配符）
# 记录值: $SLB_IP
# TTL: 600
```

或者使用阿里云 CLI：

```bash
# 添加通配符 A 记录
aliyun alidns AddDomainRecord \
  --DomainName your-domain.com \
  --RR "*" \
  --Type A \
  --Value $SLB_IP \
  --TTL 600
```

### 6. 部署测试应用

```bash
# 部署示例应用
kubectl apply -f deployments/example-deployments.yaml

# 等待 Deployment 就绪
kubectl wait --for=condition=available --timeout=60s deployment/vscode

# 检查路由是否创建
kubectl get deployment vscode -o jsonpath='{.metadata.annotations}'
```

### 7. 验证 HTTPS 访问

```bash
# 访问应用（HTTPS）
curl https://vscode.your-domain.com/

# 验证证书
curl -v https://vscode.your-domain.com/ 2>&1 | grep "subject:"
```

## 阿里云 SLB 配置说明

### SLB 规格选择

在 `caddy-k8s.yaml` 的 Service 注解中配置：

```yaml
annotations:
  # SLB 规格（根据流量选择）
  service.beta.kubernetes.io/alibaba-cloud-loadbalancer-spec: "slb.s1.small"
```

常用规格：
- `slb.s1.small`: 最大连接数 5,000，每秒新建连接数 3,000
- `slb.s2.small`: 最大连接数 50,000，每秒新建连接数 5,000
- `slb.s3.small`: 最大连接数 100,000，每秒新建连接数 10,000

### 计费方式

```yaml
annotations:
  # 按流量计费（推荐用于测试）
  service.beta.kubernetes.io/alibaba-cloud-loadbalancer-charge-type: "paybytraffic"

  # 或按带宽计费（生产环境推荐）
  # service.beta.kubernetes.io/alibaba-cloud-loadbalancer-charge-type: "paybybandwidth"
  # service.beta.kubernetes.io/alibaba-cloud-loadbalancer-bandwidth: "100"  # 带宽 100Mbps
```

### 使用已有 SLB

如果您已有 SLB 实例，可以直接使用：

```yaml
annotations:
  # 指定已有 SLB ID
  service.beta.kubernetes.io/alibaba-cloud-loadbalancer-id: "lb-xxxxxxxxxx"
```

### 内网 SLB

如果只需要内网访问：

```yaml
annotations:
  # 内网 SLB
  service.beta.kubernetes.io/alibaba-cloud-loadbalancer-address-type: "intranet"
```

## TLS 证书自动签发

Caddy 会自动通过阿里云 DNS API 验证域名所有权并签发 Let's Encrypt 证书：

1. **首次部署**：证书签发需要 1-2 分钟
2. **自动续期**：证书到期前 30 天自动续期
3. **通配符证书**：支持 `*.your-domain.com` 通配符证书

### 证书存储

Caddy 会将证书存储在容器内的 `/data/caddy` 目录。如果需要持久化存储：

```yaml
# 在 Deployment 中添加 PersistentVolumeClaim
volumeMounts:
  - name: caddy-data
    mountPath: /data/caddy

volumes:
  - name: caddy-data
    persistentVolumeClaim:
      claimName: caddy-data-pvc
```

## 故障排查

### 1. SLB 创建失败

```bash
# 查看 Service 事件
kubectl describe svc caddy-k8s

# 常见原因：
# - RAM 权限不足
# - SLB 规格不可用
# - 账户余额不足
```

### 2. 证书签发失败

```bash
# 查看 Caddy 日志
kubectl logs -f deployment/caddy-k8s | grep -i "acme\|tls\|certificate"

# 常见原因：
# - DNS API 权限不足
# - DNS 解析未生效
# - Let's Encrypt 速率限制
```

### 3. 路由无法访问

```bash
# 检查 SLB IP
kubectl get svc caddy-k8s

# 检查 DNS 解析
dig vscode.your-domain.com

# 检查路由是否创建
kubectl logs -f deployment/caddy-k8s | grep "Route created"
```

### 4. 健康检查失败

```bash
# 查看 Pod 状态
kubectl describe pod -l app=caddy-k8s

# 检查健康检查端点
kubectl exec -it deployment/caddy-k8s -- curl -k https://localhost/healthz
```

## 生产环境优化

### 1. 使用阿里云 ACR 镜像加速

```bash
# 拉取镜像时使用 ACR 加速
docker pull registry.cn-hangzhou.aliyuncs.com/your-namespace/caddy-k8s:latest
```

### 2. 配置 HPA（水平自动扩缩容）

虽然插件建议单副本运行，但可以通过以下方式提高可用性：

```yaml
# 配置反亲和性，将 Pod 分散到不同节点
affinity:
  podAntiAffinity:
    preferredDuringSchedulingIgnoredDuringExecution:
      - weight: 100
        podAffinityTerm:
          labelSelector:
            matchExpressions:
              - key: app
                operator: In
                values:
                  - caddy-k8s
          topologyKey: kubernetes.io/hostname
```

### 3. 监控和告警

集成阿里云 ARMS（应用实时监控服务）：

```bash
# 启用 Prometheus 监控（Caddy 原生支持）
# 在 Caddyfile 中添加：
# {
#   servers {
#     metrics
#   }
# }
```

### 4. 日志收集

使用阿里云日志服务 SLS：

```yaml
# 添加 SLS 采集配置
metadata:
  annotations:
    aliyun.logs.caddy: "stdout"
    aliyun.logs.caddy.product: "ACK"
```

## 成本优化

1. **使用按流量计费的 SLB**（测试环境）
2. **选择合适的 SLB 规格**（避免过度配置）
3. **配置资源限制**（避免资源浪费）
4. **使用 Spot 实例**（非关键工作负载）

## 安全建议

1. **使用 RAM 最小权限原则**
   - 仅授予必要的 DNS API 权限
   - 定期轮换 AccessKey

2. **启用 SLB 访问控制**
   ```yaml
   annotations:
     # 配置白名单（仅允许特定 IP 访问）
     service.beta.kubernetes.io/alibaba-cloud-loadbalancer-acl-id: "acl-xxxxxxxxxx"
     service.beta.kubernetes.io/alibaba-cloud-loadbalancer-acl-status: "on"
     service.beta.kubernetes.io/alibaba-cloud-loadbalancer-acl-type: "white"
   ```

3. **启用 Pod 安全策略**
   - 使用非 root 用户运行容器
   - 启用只读根文件系统

## 参考文档

- [阿里云 ACK 文档](https://help.aliyun.com/product/85222.html)
- [阿里云 SLB 注解说明](https://help.aliyun.com/document_detail/86531.html)
- [阿里云 DNS API](https://help.aliyun.com/document_detail/29739.html)
- [Caddy 阿里云 DNS 插件](https://github.com/caddy-dns/alidns)
