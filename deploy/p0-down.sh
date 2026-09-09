#!/bin/bash
# P0 环境销毁：删 kind 集群与 registry 容器。
set -euo pipefail
DIR="$(cd "$(dirname "$0")" && pwd)"
KIND="${KIND:-kind}"
command -v "$KIND" >/dev/null || KIND="$HOME/.workbuddy/binaries/bin/kind"
"$KIND" delete cluster --name sdp-dev || true
docker rm -f kind-registry 2>/dev/null || true
echo "[p0] torn down"
