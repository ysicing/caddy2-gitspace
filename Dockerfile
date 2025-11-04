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

RUN apk --no-cache add ca-certificates tzdata

WORKDIR /root/

# 复制编译好的 Caddy
COPY --from=builder /app/caddy .

EXPOSE 80 443 2019

CMD ["./caddy", "run", "--config", "/etc/caddy/Caddyfile"]
