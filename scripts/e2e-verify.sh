#!/usr/bin/env bash
# hub e2e-verify.sh — 真跑业务端点 E2E（hands and eyes），落外部证据。
# 连已部署的 hub（K8s 网关），Flow A 登录拿 session cookie，打只读业务端点，
# 落请求/响应到 evidence/ 带时间戳 + sha256；不止 /health。零新依赖（curl + python3）。
# 用法: bash scripts/e2e-verify.sh
set -uo pipefail

REPO_DIR="$(cd "$(dirname "$0")/.." && pwd)"
STAMP="$(date -u +%FT%TZ | tr ':' '-')"
OUT="$REPO_DIR/evidence/$STAMP"
GW="${E2E_GATEWAY:-https://www.sdpworkflow.com:8443}"
RESOLVE="${E2E_RESOLVE:-www.sdpworkflow.com:8443:127.0.0.1}"
KC_ADMIN="${E2E_KC_ADMIN:-admin}"
KC_PASS="${E2E_KC_PASS:-sdp@12345}"

CURL=(curl -sk --noproxy '*' --max-time 15 --resolve "$RESOLVE")
mkdir -p "$OUT/responses"
# session cookie 仅存于临时文件，退出即销毁；evidence/ 不留凭据。
JAR="$(mktemp "${TMPDIR:-/tmp}/sdp-e2e-cookie.XXXXXX")"
trap 'rm -f "$JAR"' EXIT
LOG="$OUT/e2e.log"
: > "$LOG"
say() { echo "[$(date -u +%FT%TZ)] $*" | tee -a "$LOG"; }
sha() { shasum -a 256 "$1" 2>/dev/null | awk '{print $1}'; }

# 1) Flow A 登录（服务端做 token exchange + 下发 session cookie；cookie 路径不校验 bearer azp）。
say "登录 hub (Flow A) -> $GW/api/v1/auth/login"
login_code=$("${CURL[@]}" -c "$JAR" -o "$OUT/login.json" -w '%{http_code}' \
  -X POST "$GW/api/v1/auth/login" -H 'Content-Type: application/json' \
  -d "{\"username\":\"${KC_ADMIN}\",\"password\":\"${KC_PASS}\"}" 2>/dev/null)
echo "login_http=$login_code" >> "$LOG"
if [ "$login_code" != "200" ]; then
  say "FAIL 登录失败 http=$login_code: $(head -c 200 "$OUT/login.json")"
  exit 1
fi
say "PASS 登录 http=200 cookie=$(grep -c 'sdp_sess' "$JAR" 2>/dev/null || echo 0)"

# 2) 打真实业务端点（只读），落请求与响应体。
ENDPOINTS=(
  "/api/v1/ops/health|ops-health"
  "/api/v1/targets|targets"
  "/api/v1/runs|runs"
  "/api/v1/notifications|notifications"
)
failures=0
manifest='['
first=1
for spec in "${ENDPOINTS[@]}"; do
  path="${spec%%|*}"; label="${spec##*|}"
  say "驱动 $path"
  body="$OUT/responses/$label.json"
  code=$("${CURL[@]}" -b "$JAR" -o "$body" -w '%{http_code}' "$GW$path" 2>/dev/null)
  bytes=$(wc -c < "$body" | tr -d ' ')
  ok=0
  if [ "$code" = "200" ] && [ "$bytes" -gt 20 ]; then ok=1; else failures=$((failures+1)); fi
  say "  $([ $ok -eq 1 ] && echo PASS || echo FAIL) $path http=$code bytes=$bytes sha=$(sha "$body" | cut -c1-12)"
  [ $first -eq 0 ] && manifest+=','
  first=0
  manifest+="{\"endpoint\":\"$path\",\"label\":\"$label\",\"http\":$code,\"bytes\":$bytes,\"ok\":$ok,\"sha256\":\"$(sha "$body")\",\"captured_at\":\"$(date -u +%FT%TZ)\"}"
done
manifest+=']'

# 3) manifest：整条链与失败计数。
python3 - "$OUT/manifest.json" "$manifest" "$failures" "$GW" <<'PY'
import json, sys
out, raw, failures, gw = sys.argv[1], sys.argv[2], int(sys.argv[3]), sys.argv[4]
data = {
    "stamp": out.rsplit('/', 1)[-1],
    "gateway": gw,
    "chain": "flow-a-login -> session-cookie(temp only) -> GET business-endpoints -> evidence",
    "endpoints": json.loads(raw),
    "failures": failures,
}
json.dump(data, open(out, 'w'), ensure_ascii=False, indent=2)
PY
cp "$LOG" "$OUT/e2e-final.log" 2>/dev/null || true
say "E2E $([ $failures -eq 0 ] && echo PASS || echo FAIL) failures=$failures evidence=$OUT"
[ $failures -gt 0 ] && exit 1
exit 0
