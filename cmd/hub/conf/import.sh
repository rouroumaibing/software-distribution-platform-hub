#!/usr/bin/env bash
# 将 conf/*.sql 按文件名顺序导入集群内 Postgres(sdp-system/postgres)。
# 用法: bash software-distribution-platform-hub/cmd/hub/conf/import.sh
# 幂等: 全部语句带 ON CONFLICT DO NOTHING, 重复导入安全。
set -euo pipefail

CONF_DIR="$(cd "$(dirname "$0")" && pwd)"
# devops 工作区根(约定 .kubeconfig 放在这里)
ROOT_DIR="$(cd "$CONF_DIR/../../../.." && pwd)"
export KUBECONFIG="$ROOT_DIR/.kubeconfig"

for f in "$CONF_DIR"/*.sql; do
  echo "== importing $(basename "$f") =="
  kubectl -n sdp-system exec -i deploy/postgres -- psql -v ON_ERROR_STOP=1 -U sdp -d sdp < "$f"
done

echo "done. 验证: kubectl -n sdp-system exec -it deploy/postgres -- psql -U sdp -d sdp -c 'select count(*) from components;'"
