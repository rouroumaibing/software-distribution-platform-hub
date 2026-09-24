#!/usr/bin/env bash
# =====================================================================
# hack/migration-check.sh —— 用一次性 Postgres 验证「两条建库路径」全绿。
#
# 为什么需要它（2026-09-23 新增）:
#   本仓的 DB-free 门禁（go build / vet / test / GORM DryRun）**看不到**迁移是否
#   真能执行 —— DryRun 只捕获 SQL 文本，不碰 Postgres。实测在真实 PG 16 上一次
#   就跑出 3 个纸面审查看不出的缺陷（0015 两个 + 0001 的形状分歧），全都不是编译
#   错误、全都在启动时才爆。故把"真库跑一遍"固化成脚本，作为迁移类改动的前置门禁。
#
# 验证的路径:
#   Path A 全新安装（= deploy-local.sh 的实际路径）
#     AutoMigrate ──► migrations/0006..0016
#   Path B 老库升级（= 已上线环境升到新版）
#     migrations/0001..0005 ──► 灌旧数据 ──► migrations/0015 ──► AutoMigrate ──► 0006..0016
#
#   ⚠️ Path B 的顺序是实测结论，与 0015 早先的注释相反: AutoMigrate **自己**也没法在
#      老库上跑 —— 它要把 component_configs.created_by 改成 varchar，而指向 users(id)
#      的 FK 还在，Postgres 直接拒绝（42804）。所以必须**先跑 0015 摘 FK + 回填**，
#      再启动新版 hub。（0015 自带 ADD COLUMN IF NOT EXISTS owner_sub，不依赖
#      AutoMigrate 先跑。）
#
# 用法: bash hack/migration-check.sh
#   环境变量: PG_IMAGE(默认 postgres:16-alpine) CHECK_PORT(默认 55433)
#   依赖: docker、go。不依赖本机 psql（一律 docker exec 进容器）。
# =====================================================================
set -euo pipefail

APP_DIR="$(cd "$(dirname "$0")/.." && pwd)"
cd "$APP_DIR"

PG_IMAGE="${PG_IMAGE:-postgres:16-alpine}"
CHECK_PORT="${CHECK_PORT:-55433}"
CT="sdp-migration-check"
BIN="$(mktemp -t hub-migcheck.XXXXXX)"
LOGDIR="$(mktemp -d -t hub-migcheck-logs.XXXXXX)"

cleanup() {
    kill "${HUB_PID:-}" 2>/dev/null || true
    docker rm -f "$CT" >/dev/null 2>&1 || true
    rm -f "$BIN"
    echo "[migration-check] 日志留在: $LOGDIR"
}
trap cleanup EXIT

fail() { echo "[migration-check] FAIL: $*" >&2; exit 1; }

# ---------------------------------------------------------------- 准备
command -v docker >/dev/null || fail "需要 docker"
docker info >/dev/null 2>&1 || fail "docker daemon 未运行"

echo "[migration-check] 构建 hub 二进制…"
go build -o "$BIN" ./cmd/hub

# 注意: 变量后紧跟非 ASCII 字符（如 … / 中文）必须写 ${VAR} —— 否则多字节字符会被
#      当作变量名的一部分，在 set -u 下直接 unbound variable 崩掉（本项目踩过）。
echo "[migration-check] 启动一次性 Postgres (${PG_IMAGE}) 于 127.0.0.1:${CHECK_PORT}"
docker rm -f "$CT" >/dev/null 2>&1 || true
docker run -d --name "$CT" \
    -e POSTGRES_USER=sdp -e POSTGRES_PASSWORD=sdp -e POSTGRES_DB=sdp \
    -e POSTGRES_HOST_AUTH_METHOD=trust \
    -p "127.0.0.1:$CHECK_PORT:5432" "$PG_IMAGE" >/dev/null

for _ in $(seq 1 40); do
    docker exec "$CT" pg_isready -U sdp -d sdp >/dev/null 2>&1 && break
    sleep 1
done
docker exec "$CT" pg_isready -U sdp -d sdp >/dev/null 2>&1 || fail "Postgres 未就绪"

psql_on() { docker exec -i "$CT" psql -v ON_ERROR_STOP=1 -U sdp -d "$1"; }
dsn()     { echo "postgres://sdp:sdp@127.0.0.1:$CHECK_PORT/$1?sslmode=disable"; }
freshdb() { docker exec "$CT" psql -q -U sdp -d sdp -c "drop database if exists $1;" -c "create database $1;" >/dev/null; }

# 启动 hub 触发 AutoMigrate，等到「listening」或进程死亡。$1=db $2=port $3=log
run_automigrate() {
    local db="$1" port="$2" log="$3"
    DB_DSN="$(dsn "$db")" HUB_ADDR="127.0.0.1:$port" LOG_LEVEL=error "$BIN" >"$log" 2>&1 &
    HUB_PID=$!
    local ok=1
    for _ in $(seq 1 30); do
        if grep -q 'listening on' "$log" 2>/dev/null; then ok=0; break; fi
        kill -0 "$HUB_PID" 2>/dev/null || break   # 进程已退出 = 启动失败
        sleep 1
    done
    kill "$HUB_PID" 2>/dev/null || true
    wait "$HUB_PID" 2>/dev/null || true
    if [ "$ok" -ne 0 ]; then
        echo "----- $log -----" >&2; tail -n 20 "$log" >&2
        fail "AutoMigrate 在库 $db 上失败（hub 未能启动）"
    fi
}

apply_ops_set() {   # 运维集合 0006..0016（Path A 与 Path B 的末段共用）
    local db="$1" f b
    for f in migrations/000[6-9]*.sql migrations/001[0-6]*.sql; do
        b="$(basename "$f")"
        if psql_on "$db" <"$f" >/dev/null 2>"$LOGDIR/$db-$b.err"; then
            printf '    OK   %s\n' "$b"
        else
            printf '    FAIL %s\n' "$b"
            sed 's/^/         /' "$LOGDIR/$db-$b.err" | head -4
            fail "$db: $b 执行失败"
        fi
    done
}

# ---------------------------------------------------------------- Path A
echo
echo "[migration-check] ===== Path A: 全新安装（AutoMigrate → 0006..0016）====="
freshdb sdp_patha
run_automigrate sdp_patha 18071 "$LOGDIR/patha-hub.log"
T_A="$(docker exec "$CT" psql -t -A -U sdp -d sdp_patha -c \
    "select count(*) from information_schema.tables where table_schema='public' and table_type='BASE TABLE';")"
echo "    AutoMigrate 建出 $T_A 张表；users 表存在? $(docker exec "$CT" psql -t -A -U sdp -d sdp_patha -c "select coalesce(to_regclass('public.users')::text,'no');")"
apply_ops_set sdp_patha
echo "    Path A 全绿"

# ---------------------------------------------------------------- Path B
echo
echo "[migration-check] ===== Path B: 老库升级（0001..0005 → 0015 → AutoMigrate → 0006..0016）====="
freshdb sdp_pathb
for f in migrations/000[1-5]*.sql; do
    psql_on sdp_pathb <"$f" >/dev/null || fail "Path B 引导脚本 $(basename "$f") 失败"
done
echo "    引导库（0001..0005）就绪"

# 灌入能触达回填三条分支的旧数据:
#   comp-a owner/alice （keycloak_id 有值 ⇒ 应回填 kc-sub-alice）
#   comp-b owner/carol （keycloak_id 为 NULL ⇒ owner_sub 留空 + 告警）
#   comp-c owner/悬空   （components.owner_user 无 FK ⇒ 可悬空；告警）
psql_on sdp_pathb >/dev/null <<'SQL'
insert into orgs (id,name,slug) values ('11111111-1111-1111-1111-111111111111','Acme','acme');
insert into service_trees (id,org_id,name) values ('22222222-2222-2222-2222-222222222222','11111111-1111-1111-1111-111111111111','default');
insert into services (id,service_tree_id,key,name) values ('33333333-3333-3333-3333-333333333333','22222222-2222-2222-2222-222222222222','svc-a','Service A');
insert into users (id,org_id,email,name,keycloak_id) values ('44444444-4444-4444-4444-444444444444','11111111-1111-1111-1111-111111111111','alice@example.com','Alice','kc-sub-alice');
insert into users (id,org_id,email,name,keycloak_id) values ('cccccccc-cccc-cccc-cccc-cccccccccccc','11111111-1111-1111-1111-111111111111','carol@example.com','Carol',NULL);
insert into components (id,service_id,key,name,repo_url,owner_user) values ('55555555-5555-5555-5555-555555555555','33333333-3333-3333-3333-333333333333','comp-a','Component A','https://example.com/a.git','44444444-4444-4444-4444-444444444444');
insert into components (id,service_id,key,name,repo_url,owner_user) values ('bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb','33333333-3333-3333-3333-333333333333','comp-b','Component B','https://example.com/b.git','cccccccc-cccc-cccc-cccc-cccccccccccc');
insert into components (id,service_id,key,name,repo_url,owner_user) values ('dddddddd-dddd-dddd-dddd-dddddddddddd','33333333-3333-3333-3333-333333333333','comp-c','Component C','https://example.com/c.git','eeeeeeee-eeee-eeee-eeee-eeeeeeeeeeee');
insert into component_configs (id,component_id,key,value,created_by,updated_by) values ('66666666-6666-6666-6666-666666666666','55555555-5555-5555-5555-555555555555','K','V','44444444-4444-4444-4444-444444444444','44444444-4444-4444-4444-444444444444');
insert into pipelines (id,component_id,name,kind) values ('88888888-8888-8888-8888-888888888888','55555555-5555-5555-5555-555555555555','pipe-a','build');
insert into pipeline_versions (id,pipeline_id,version,snapshot,created_by) values ('aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa','88888888-8888-8888-8888-888888888888',1,'{}','44444444-4444-4444-4444-444444444444');
SQL

echo "    先跑 0015（摘 FK + 回填 + 删表）——必须先于新版 hub 启动"
psql_on sdp_pathb < migrations/0015_d3_drop_users_and_legacy_columns.sql >"$LOGDIR/pathb-0015.out" 2>&1 \
    || { sed 's/^/    /' "$LOGDIR/pathb-0015.out" | tail -10; fail "Path B: 0015 失败"; }
grep -c 'dropped FK' "$LOGDIR/pathb-0015.out" | sed 's/^/    摘除入向 FK 数: /'

# 回填断言：alice 的三处必须都变成 sub；users 表必须已删
assert_eq() { # $1=期望 $2=实际 $3=说明
    [ "$1" = "$2" ] || fail "$3: 期望 [$1] 实际 [$2]"
    echo "    OK   $3 = $1"
}
assert_eq 'kc-sub-alice' "$(docker exec "$CT" psql -t -A -U sdp -d sdp_pathb -c "select owner_sub from components where key='comp-a';")" 'comp-a.owner_sub'
assert_eq 'kc-sub-alice' "$(docker exec "$CT" psql -t -A -U sdp -d sdp_pathb -c "select created_by from component_configs where id='66666666-6666-6666-6666-666666666666';")" 'configs.created_by'
assert_eq 'kc-sub-alice' "$(docker exec "$CT" psql -t -A -U sdp -d sdp_pathb -c "select updated_by from component_configs where id='66666666-6666-6666-6666-666666666666';")" 'configs.updated_by'
assert_eq 'kc-sub-alice' "$(docker exec "$CT" psql -t -A -U sdp -d sdp_pathb -c "select created_by from pipeline_versions;")" 'pipeline_versions.created_by'
assert_eq 'no' "$(docker exec "$CT" psql -t -A -U sdp -d sdp_pathb -c "select coalesce(to_regclass('public.users')::text,'no');")" 'users 表已删除'
assert_eq '0'  "$(docker exec "$CT" psql -t -A -U sdp -d sdp_pathb -c "select count(*) from pg_constraint where contype='f' and confrelid='users'::regclass;" 2>/dev/null || echo 0)" '无残留 FK 指向 users（表已不在）'

echo "    再启动新版 hub（AutoMigrate 必须在 0015 之后才跑得动）"
run_automigrate sdp_pathb 18072 "$LOGDIR/pathb-hub.log"
apply_ops_set sdp_pathb
echo "    Path B 全绿"

# ---------------------------------------------------------------- 收敛断言
echo
echo "[migration-check] ===== 收敛断言：两条路径的 schema 差异必须都是"已知且良性"的 ====="
# 为什么不做"必须完全相同"：两条路径**注定**不能完全一致 —— AutoMigrate 只加不删，
# 所以老库里会残留模型早已移除的历史列。把这类差异列成白名单，等于把"收敛检查"
# 从一句模糊告警升级成**真门禁**：白名单内的差异放行，**任何新差异直接 FAIL**。
#
# 白名单（逐条已人工核实为良性，见 migrations/README.md）:
#   rollout_runs.created_at / updated_at
#     模型 internal/run/models/rollout_run.go **没有**嵌入时间戳基类，故 AutoMigrate
#     不建这两列；而 0001 建了。老库里它们是 `not null default now()` ⇒ GORM 的
#     INSERT 不带这两列也由 DB 默认值填上，**不会插入失败**，只是老库多两列。
#   pipeline_task_templates.environment_id
#     `0001` 的 Deploy 任务专属列；模型已无 `EnvironmentID` 字段（git grep 为空），
#     是可空的 inert 残留列，无人读写。
KNOWN_DIVERGENCES="
pipeline_task_templates.environment_id:uuid
rollout_runs.created_at:timestamp with time zone
rollout_runs.updated_at:timestamp with time zone
"

SNAPSHOT="select table_name||'.'||column_name||':'||data_type from information_schema.columns where table_schema='public' order by 1"
docker exec "$CT" psql -t -A -U sdp -d sdp_patha -c "$SNAPSHOT" >"$LOGDIR/schema-a.txt"
docker exec "$CT" psql -t -A -U sdp -d sdp_pathb -c "$SNAPSHOT" >"$LOGDIR/schema-b.txt"
# 注意: 必须把 diff 先单独落文件、再 awk。`diff | awk` 在 `set -o pipefail` 下会因为
#      diff 的「有差异 ⇒ 退出码 1」让整条管道判为失败，`set -e` 直接终止脚本 ——
#      表现为收敛块**一行输出都没有**、exit=1，看起来像"检查通过但没打印"（实测踩过）。
diff "$LOGDIR/schema-a.txt" "$LOGDIR/schema-b.txt" >"$LOGDIR/raw.diff" || true
awk '/^[<>] /{sub(/^[<>] /,""); print}' "$LOGDIR/raw.diff" | sort -u >"$LOGDIR/diverge.txt"

if [ ! -s "$LOGDIR/diverge.txt" ]; then
    echo "    OK   两路径列集合完全一致（$(wc -l <"$LOGDIR/schema-a.txt" | tr -d ' ') 列）"
else
    UNKNOWN=0
    while IFS= read -r line; do
        [ -n "$line" ] || continue
        if printf '%s\n' "$KNOWN_DIVERGENCES" | grep -qxF "$line"; then
            echo "    OK   已知良性差异: $line"
        else
            echo "    FAIL 未登记的差异: $line" >&2
            UNKNOWN=$((UNKNOWN + 1))
        fi
    done <"$LOGDIR/diverge.txt"
    [ "$UNKNOWN" -eq 0 ] || fail "出现 $UNKNOWN 处未登记的两路径 schema 差异；若确认良性请加入 KNOWN_DIVERGENCES 并在 migrations/README.md 记录原因"
fi

echo
echo "[migration-check] PASS：两条建库路径均全绿。"
