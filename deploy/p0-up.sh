#!/bin/bash
# P0 环境一键拉起：registry -> kind 集群 -> 构建镜像 -> 推送 -> 部署 -> 种子数据 -> 验证。
# 用法: ./p0-up.sh          （幂等，重复执行安全）
#       ./p0-down.sh        （销毁）
# 前提: Docker Desktop 运行中；kind 在 PATH 或 ~/.workbuddy/binaries/bin/kind。
#
# 本机环境踩过的坑（2026-09-06 P0 实测）：
# 1. 沙箱/权限：docker buildx 的 ~/.docker/buildx 与 kind 的 ~/.kube 写入会被
#    WorkBuddy 沙箱拦截 —— 全部用 DOCKER_CONFIG / KUBECONFIG 重定向到 workspace。
# 2. hub go.mod 有 replace ../software-distribution-platform-runner —— hub 镜像
#    必须以 workspace 根为构建上下文（根 .dockerignore 已收窄）。
# 3. kind 节点继承 Docker Desktop 的 HTTP_PROXY（127.0.0.1:port 节点内不可达），
#    集群创建后必须清掉节点代理环境并重启 containerd。
# 4. 节点拉 docker.io 会被代理坑死 —— postgres 也推到本地 registry。
# 5. dev 镜像同 tag 反复覆盖，imagePullPolicy 必须 Always。
set -euo pipefail

DIR="$(cd "$(dirname "$0")" && pwd)"
ROOT="$(cd "$DIR/../.." && pwd)"
HUB_DIR="$ROOT/software-distribution-platform-hub"
RUNNER_DIR="${RUNNER_DIR:-$ROOT/software-distribution-platform-runner}"
KUBECONFIG="$ROOT/.kubeconfig"
export KUBECONFIG
export DOCKER_CONFIG="$ROOT/.dockerconfig"
KIND="${KIND:-kind}"
command -v "$KIND" >/dev/null || KIND="$HOME/.workbuddy/binaries/bin/kind"
TAG="dev"
TOKEN="sdp-dev-token-2026"

log() { echo "[p0] $*"; }

# 1. registry（跑在 kind docker 网络里，节点经 mirror 访问，主机经 127.0.0.1:5000 push）
docker network create kind 2>/dev/null || true
docker inspect kind-registry >/dev/null 2>&1 || \
  docker run -d --restart=always --name kind-registry --network kind \
    -p 127.0.0.1:5000:5000 registry:2
log "registry ready (127.0.0.1:5000)"

# 2. kind 集群（幂等）
"$KIND" get clusters 2>/dev/null | grep -qx sdp-dev || \
  "$KIND" create cluster --config "$DIR/kind.yaml"
"$KIND" export kubeconfig --name sdp-dev

# 2.5 清节点代理环境（kind config 无法注入 env，只能创建后修）
NODE="sdp-dev-control-plane"
docker exec "$NODE" bash -c "systemctl set-environment HTTP_PROXY= HTTPS_PROXY= http_proxy= https_proxy= NO_PROXY='*' no_proxy='*' && systemctl restart containerd" || true
log "node proxy env cleared"

# 3. 构建镜像（hub 用根上下文满足 go.mod replace；goproxy.cn）
log "building images (first run takes a few minutes)..."
docker build -f "$HUB_DIR/deploy/Dockerfile" -t "localhost:5000/hub:$TAG" "$ROOT"
docker build -f "$RUNNER_DIR/deploy/Dockerfile" -t "localhost:5000/runner:$TAG" "$RUNNER_DIR"
log "images built"

# 4. 推送（节点经 containerd mirror 从 kind-registry 拉取）
docker push -q "localhost:5000/hub:$TAG" >/dev/null
docker push -q "localhost:5000/runner:$TAG" >/dev/null
# postgres 也走本地 registry（节点拉不了 docker.io）
docker pull -q postgres:16-alpine >/dev/null
docker tag postgres:16-alpine "localhost:5000/postgres:16-alpine"
docker push -q "localhost:5000/postgres:16-alpine" >/dev/null
log "images pushed"

# 5. 部署
kubectl apply -f "$DIR/manifests/"
log "manifests applied"

# 6. 等待就绪
kubectl -n sdp-system rollout status deploy/postgres --timeout=180s
kubectl -n sdp-system rollout status deploy/hub --timeout=180s
kubectl -n sdp-system rollout status deploy/runner --timeout=180s

# 7. 种子数据：注册 runner 对应的集群（幂等：409/已存在则跳过）
curl -s -o /dev/null -X POST http://localhost:8080/api/v1/clusters \
  -H "Content-Type: application/json" \
  -d '{"name":"local-dev","vendor":"kind","region":"local"}' || true

# 8. 验证 gateway 握手（L2-1）
sleep 35  # runner 重连退避最长 32s
log "=== gateway handshake check ==="
kubectl -n sdp-system logs deploy/hub --tail=50 | grep -i "connected" | tail -1 || \
  { log "handshake not seen yet, runner logs:"; kubectl -n sdp-system logs deploy/runner --tail=5; exit 1; }
curl -s http://localhost:8080/api/v1/clusters | grep -o '"status":"[a-z]*"' | head -1
log "P0 done. hub API: http://localhost:8080/api/v1 ; console vite proxy 无需改动。"
