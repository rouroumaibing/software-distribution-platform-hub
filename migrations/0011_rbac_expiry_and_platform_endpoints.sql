-- =====================================================================
-- 0011_rbac_expiry_and_platform_endpoints.sql
--   platform_role_bindings / component_role_bindings 补 expires_at
--   运行: 手动执行（与 0004~0010 同路径）。语句幂等，可重复跑。
--     kubectl -n sdp-workflow exec -i deploy/postgres -- \
--       psql -v ON_ERROR_STOP=1 -U sdp -d sdp < migrations/0011_rbac_expiry_and_platform_endpoints.sql
--
-- 背景:
--   ACCOUNT-PERMISSION-MODEL §7.4 的「审批通过后写绑定 + 到期回收」要求绑定带
--   TTL；§10 #15 记「平台级授予不可达」并把它与 C-10（平台级 RBAC HTTP 端点）
--   排在同一批（§11 步骤 3）。本迁移补列，C-10 的端点在同批代码里落地。
--
-- 说明:
--   * 列同时由 AutoMigrate 补（模型已声明 *time.Time + index 标签），本文件也
--     显式 add column / create index 各一次，让「纯 AutoMigrate 建库」与
--     「手跑 SQL 建库」两条路径收敛。
--   * **到期语义落在查询层**：repository 的 ListMatching 加
--     `expires_at IS NULL OR expires_at > now()`。这是唯一被鉴权中间件使用的
--     解析路径 —— 只在 API 边界过滤会让「已过期的绑定仍然授权」。
--     过期行**不删**（审计仍要看到曾经授过什么），由 §7.4 的回收作业另行清理。
--   * §5.3 的主体语义**同时写进 DDL**：应用层校验挡不住手工 SQL，故加 CHECK。
--     component_role_bindings 的 subject_type 可空（V1 遗留行只有 user_id），
--     因此那条 CHECK 容忍 NULL。
--
-- ⚠️ 若下面第 4 步报 check constraint violated:
--   说明库里已有不符合 §5.3 的行（例如 subject_type 写了别的值、或组写成了
--   `sdp-admins` 缺前导斜杠）。**先查再改**，不要盲目放宽约束:
--     select id, subject_type, subject_id from platform_role_bindings
--      where subject_type not in ('user','group');
--   列本身在第 1~2 步已加好，约束失败不影响补列结果，修完数据重跑本文件即可。
-- =====================================================================

-- 1) platform_role_bindings: expires_at（NULL = 永久）
alter table platform_role_bindings
    add column if not exists expires_at timestamptz;

-- 2) component_role_bindings: expires_at（NULL = 永久）
alter table component_role_bindings
    add column if not exists expires_at timestamptz;

-- 3) 索引：主体查找（鉴权热路径）与到期扫描（回收作业）
create index if not exists idx_platform_role_bindings_subject
    on platform_role_bindings (subject_type, subject_id);
create index if not exists idx_platform_role_bindings_expires_at
    on platform_role_bindings (expires_at);
create index if not exists idx_component_role_bindings_expires_at
    on component_role_bindings (expires_at);

-- 4) §5.3 主体语义写入 DDL（幂等：先 drop 再 add）
alter table platform_role_bindings
    drop constraint if exists platform_role_bindings_subject_type_check;
alter table platform_role_bindings
    add constraint platform_role_bindings_subject_type_check
    check (subject_type in ('user', 'group'));

-- component_role_bindings.subject_type 可空（V1 遗留行），故容忍 NULL
alter table component_role_bindings
    drop constraint if exists component_role_bindings_subject_type_check;
alter table component_role_bindings
    add constraint component_role_bindings_subject_type_check
    check (subject_type is null or subject_type in ('user', 'group'));

-- 5) 组主体必须带前导斜杠（groups mapper 设了 full.path=true；见 §5.3 反例
--    「写 sdp-admins 而 claim 是 /sdp-admins ⇒ 静默不匹配」）。
--    只对非空且 subject_type='group' 的行校验。
alter table platform_role_bindings
    drop constraint if exists platform_role_bindings_group_path_check;
alter table platform_role_bindings
    add constraint platform_role_bindings_group_path_check
    check (subject_type <> 'group' or subject_id like '/%');
