#!/bin/bash
# 构建包含 k8s_router 和 阿里云 DNS 插件的 Caddy

set -e

echo "Building Caddy with k8s_router and Alibaba Cloud DNS plugin..."

xcaddy build \
  --with github.com/ysicing/caddy2-gitspace=. \
  --with github.com/caddy-dns/alidns

echo "Build complete: ./caddy"
echo ""
echo "Verify DNS module is available:"
./caddy list-modules | grep dns
echo ""
echo "Next steps:"
echo "1. Test locally: ./caddy run --config ./Caddyfile.local"
echo "2. Build Docker image: docker build -t your-registry/caddy-k8s:latest ."
