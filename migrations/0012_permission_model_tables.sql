-- =====================================================================
-- 0012_permission_model_tables.sql
--   ACCOUNT-PERMISSION-MODEL §3 / §5.1③ / §6 / §7.2 的四张表
--     resource_ownership / role_api_mappings / audit_log / permission_requests
--   运行: 手动执行（与 0004~0011 同路径）。语句幂等，可重复跑。
--     kubectl -n sdp-workflow exec -i deploy/postgres -- \
--       psql -v ON_ERROR_STOP=1 -U sdp -d sdp < migrations/0012_permission_model_tables.sql
--
-- 背景（plans/UNIMPLEMENTED-MODULES-PLAN.md 第八批）:
--   这四张表是 §11 步骤 3 的其余部分，与 C-10 平台级端点同属「表结构」一批：
--     * resource_ownership  —— 资源归属的权威源（§3）。既有 component_role_bindings
--       上的 org_id 冗余列降级为派生缓存（可重建、不可手改）。
--     * role_api_mappings   —— 「角色 → 接口」映射落表（§5.1③），让某角色能打
--       哪些接口可被独立审阅与对账，而不是散在路由注册语句里。
--     * audit_log           —— 写操作审计单表（§6）。由中间件统一写，业务零手写。
--     * permission_requests —— 权限申请审批流（§7.2）。与 pipeline_approvals
--       是两件事（后者是流水线审批门），两者只通过数据对接。
--
-- 说明:
--   * 表同时由 AutoMigrate 创建（模型已注册进 db.Open），本文件让「纯
--     AutoMigrate 建库」与「手跑 SQL 建库」两条路径收敛。
--   * §5.3 的主体语义同样写进 DDL（应用层校验挡不住手工 SQL）。
-- =====================================================================

-- 1) resource_ownership —— 归属权威源（§3）
create table if not exists resource_ownership (
    id            uuid primary key default gen_random_uuid(),
    resource_type varchar(32) not null,
    resource_id   uuid        not null,
    owner_org     uuid,
    allowed_orgs  jsonb,
    created_at    timestamptz not null default now()
);
create index if not exists idx_resource_ownership_type    on resource_ownership (resource_type);
create index if not exists idx_resource_ownership_res     on resource_ownership (resource_id);
create index if not exists idx_resource_ownership_owner   on resource_ownership (owner_org);
-- 一个资源只有一条归属（单属主 + 共享集）。
create unique index if not exists uq_resource_ownership_res on resource_ownership (resource_type, resource_id);

-- 2) role_api_mappings —— 角色 → 接口映射（§5.1③）
create table if not exists role_api_mappings (
    id          uuid primary key default gen_random_uuid(),
    role_kind   varchar(16) not null,
    role_id     uuid        not null,
    action      varchar(64) not null,
    description text
);
create index if not exists idx_role_api_mappings_kind on role_api_mappings (role_kind);
create index if not exists idx_role_api_mappings_role on role_api_mappings (role_id);
create unique index if not exists uq_role_api_mappings on role_api_mappings (role_kind, role_id, action);
alter table role_api_mappings
    drop constraint if exists role_api_mappings_role_kind_check;
alter table role_api_mappings
    add constraint role_api_mappings_role_kind_check
    check (role_kind in ('platform', 'component'));

-- 3) audit_log —— 写操作审计（§6）
create table if not exists audit_log (
    id            uuid primary key default gen_random_uuid(),
    "timestamp"   timestamptz not null default now(),
    subject_type  varchar(16),
    subject       varchar(128),
    roles         varchar(512),
    source_ip     varchar(64),
    action        varchar(64),
    resource_type varchar(32),
    resource_id   varchar(64),
    result        varchar(16),
    status_code   integer,
    detail        text
);
create index if not exists idx_audit_log_timestamp on audit_log ("timestamp");
create index if not exists idx_audit_log_subject   on audit_log (subject);
create index if not exists idx_audit_log_action    on audit_log (action);
create index if not exists idx_audit_log_resource  on audit_log (resource_id);

-- 4) permission_requests —— 权限申请审批流（§7.2）
create table if not exists permission_requests (
    id                      uuid primary key default gen_random_uuid(),
    requester               varchar(128) not null,
    requested_role_id       uuid         not null,
    requested_role_kind     varchar(16)  not null,
    requested_resource_type varchar(32),
    requested_resource_id   uuid,
    reason                  text         not null,
    approver                varchar(128),
    status                  varchar(16)  not null default 'Pending',
    effective_at            timestamptz,
    expires_at              timestamptz,
    created_at              timestamptz  not null default now(),
    decided_at              timestamptz
);
create index if not exists idx_permission_requests_requester on permission_requests (requester);
create index if not exists idx_permission_requests_role      on permission_requests (requested_role_id);
create index if not exists idx_permission_requests_status    on permission_requests (status);
alter table permission_requests
    drop constraint if exists permission_requests_status_check;
alter table permission_requests
    add constraint permission_requests_status_check
    check (status in ('Pending', 'Approved', 'Rejected'));
alter table permission_requests
    drop constraint if exists permission_requests_role_kind_check;
alter table permission_requests
    add constraint permission_requests_role_kind_check
    check (requested_role_kind in ('platform', 'component'));
