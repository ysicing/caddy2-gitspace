# 生产环境 HTTPS 配置指南

本文档说明如何在生产环境中为 `caddy-k8s-router` 配置 HTTPS。

## TL;DR

`k8s_router` 作为全局 app 运行，通过 Caddy Admin API 动态注入路由。对于 HTTPS，有两种方案：

1. **通配符证书 + DNS-01 challenge**（推荐）- 一次申请 `*.example.com` 证书
2. **单域名证书 + HTTP-01 challenge** - 每个 Deployment 申请单独的证书

## 架构说明

### k8s_router 的工作原理

```
┌─────────────────────────────────────────────────────────────┐
│                      Caddy 配置结构                          │
├─────────────────────────────────────────────────────────────┤
│                                                              │
│  全局配置块 {}                                                │
│  ├── k8s_router (作为 app 运行)                              │
│  │   └── 监听 K8s Deployment 变化                           │
│  │       └── 通过 Admin API 动态注入/删除路由                │
│  └── email (ACME 邮箱)                                       │
│                                                              │
│  HTTP Server (*.example.com)                                │
│  ├── 静态路由 (Caddyfile 定义)                               │
│  │   ├── /healthz → 200 OK                                  │
│  │   └── /* → 404 (兜底)                                     │
│  │                                                           │
│  └── 动态路由 (k8s_router 通过 API 注入)                      │
│      ├── app1.example.com → Pod IP:8089                     │
│      ├── app2.example.com → Pod IP:8089                     │
│      └── ...                                                 │
│                                                              │
└─────────────────────────────────────────────────────────────┘
```

**关键点**：
- 静态配置的 `*.example.com` 作为**通配符兜底**
- 动态注入的路由（如 `app1.example.com`）优先级**更高**
- HTTPS 证书由 **静态配置的站点** 申请（通配符证书）

## 方案 1：通配符证书 + DNS-01 Challenge（推荐）

### 优点
- ✅ 一次申请 `*.example.com` 证书，覆盖所有子域名
- ✅ 动态注入的路由自动继承 HTTPS
- ✅ 不需要开放 80 端口用于验证
- ✅ 适用于内网环境

### 缺点
- ⚠️ 需要 DNS 提供商 API 凭证
- ⚠️ 需要编译 DNS 插件（如 alidns）

### 步骤

#### 1. 构建包含 DNS 插件的 Caddy

```bash
# 使用提供的脚本
./build-with-dns.sh

# 或手动构建
xcaddy build \
  --with github.com/ysicing/caddy2-gitspace=. \
  --with github.com/caddy-dns/alidns

# 验证 DNS 模块已加载
./caddy list-modules | grep dns
```

支持的 DNS 提供商：
- 阿里云：`github.com/caddy-dns/alidns`
- Cloudflare：`github.com/caddy-dns/cloudflare`
- AWS Route53：`github.com/caddy-dns/route53`
- 更多：https://github.com/caddy-dns

#### 2. 配置 Caddyfile

在 `deployments/caddy-k8s.yaml` 中取消注释方案 1：

```caddyfile
{
  email admin@example.com

  k8s_router {
    namespace default
    base_domain example.com
    default_port 8089
  }
}

*.example.com {
  tls {
    dns alidns {
      access_key_id {$ALICLOUD_ACCESS_KEY}
      access_key_secret {$ALICLOUD_SECRET_KEY}
    }
  }

  handle /healthz {
    respond "OK" 200
  }

  respond "No matching deployment" 404
}

http://*.example.com {
  redir https://{host}{uri} permanent
}
```

#### 3. 创建 DNS API Secret

```bash
kubectl create secret generic alicloud-dns-secret \
  --from-literal=access-key-id=YOUR_ACCESS_KEY_ID \
  --from-literal=access-key-secret=YOUR_ACCESS_KEY_SECRET \
  -n default
```

#### 4. 部署

```bash
kubectl apply -f deployments/caddy-k8s.yaml
```

#### 5. 验证

```bash
# 检查证书
curl -v https://app1.example.com/healthz

# 查看日志
kubectl logs -f deployment/caddy-k8s -n default
```

---

## 方案 2：单域名证书 + HTTP-01 Challenge

### 优点
- ✅ 无需 DNS API 凭证
- ✅ 无需额外编译 DNS 插件
- ✅ Let's Encrypt 默认支持

### 缺点
- ⚠️ 每个域名单独申请证书
- ⚠️ 需要开放 80 端口用于 HTTP-01 验证
- ⚠️ 不适用于内网环境
- ⚠️ **当前实现不支持**（需要修改代码）

### 为什么当前实现不支持？

`k8s_router` 通过 Admin API 动态注入的路由配置是纯 JSON：

```json
{
  "@id": "k8s-route-app1",
  "match": [{"host": ["app1.example.com"]}],
  "handle": [{"handler": "reverse_proxy", "upstreams": [...]}]
}
```

**没有 TLS 配置**，因此 Caddy 不会为这些域名申请证书。

### 如何支持方案 2？（需要修改代码）

修改 `router/admin_client.go`，让 Caddy 为动态域名申请证书：

**选项 A**：在 Caddyfile 中预定义每个可能的域名（不现实）

**选项 B**：通过 Admin API 注入带 TLS 的完整站点配置（复杂）

**选项 C**（推荐）：使用方案 1，一劳永逸

---

## 推荐配置

对于生产环境，**强烈推荐方案 1**：

| 环境 | 配置 |
|-----|------|
| **本地测试** | HTTP only（`auto_https off`） |
| **生产环境** | 通配符证书 + DNS-01 challenge |

### 最小化配置示例

```caddyfile
{
  email admin@example.com
  k8s_router {
    namespace production
    base_domain example.com
    default_port 8089
  }
}

*.example.com {
  tls {
    dns alidns {
      access_key_id {$ALICLOUD_ACCESS_KEY}
      access_key_secret {$ALICLOUD_SECRET_KEY}
    }
  }

  handle /healthz { respond "OK" 200 }
  respond "No matching deployment" 404
}

http://*.example.com {
  redir https://{host}{uri} permanent
}
```

---

## 常见问题

### 1. 为什么不直接在动态路由中配置 TLS？

因为 `k8s_router` 通过 Admin API 注入的是**路由级别**的配置，不是**站点级别**的配置。TLS 证书申请需要在站点级别配置。

### 2. 通配符证书会覆盖动态路由吗？

会！这正是方案 1 的核心优势：
- Caddyfile 定义 `*.example.com` 并申请通配符证书
- `k8s_router` 注入具体域名路由（如 `app1.example.com`）
- Caddy 自动将通配符证书应用到所有匹配的子域名

### 3. 如何验证证书是否正确？

```bash
# 检查证书详情
echo | openssl s_client -connect app1.example.com:443 -servername app1.example.com 2>/dev/null | openssl x509 -text -noout

# 验证 Subject Alternative Names
echo | openssl s_client -connect app1.example.com:443 -servername app1.example.com 2>/dev/null | openssl x509 -text -noout | grep DNS
```

### 4. 证书存储在哪里？

Caddy 默认将证书存储在容器的 `/data/caddy` 目录。建议挂载 PersistentVolume：

```yaml
volumeMounts:
  - name: caddy-data
    mountPath: /data
volumes:
  - name: caddy-data
    persistentVolumeClaim:
      claimName: caddy-data-pvc
```

---

## 参考资料

- [Caddy Automatic HTTPS](https://caddyserver.com/docs/automatic-https)
- [Caddy DNS Challenge](https://caddyserver.com/docs/caddyfile/directives/tls#dns)
- [caddy-dns Providers](https://github.com/caddy-dns)
- [Let's Encrypt DNS-01 Challenge](https://letsencrypt.org/docs/challenge-types/#dns-01-challenge)
