-- =====================================================================
-- 组件维度扩展: 环境类型 / 配置管理 / 产物管理 / 流水线版本化 / 权限管理
-- =====================================================================

-- ---------------------------------------------------------------------
-- 1. 环境管理: 区分测试环境 / 生产环境
-- ---------------------------------------------------------------------
alter table environments
    add column env_type varchar(16) not null default 'test'
        check (env_type in ('test', 'production'));
-- beta/alpha/gamma 这类环境本身仍是 test 类型的具体实例;
-- env_type 主要用于平台级策略判断(比如"生产环境部署强制要求审批",
-- 这条策略在 Hub 组装 PipelineRunSpec 时校验,不依赖流水线作者是否配置了审批阶段)。

-- ---------------------------------------------------------------------
-- 2. 用户与角色(权限管理的基础)
-- ---------------------------------------------------------------------
create table users (
    id          uuid primary key default gen_random_uuid(),
    org_id      uuid not null references orgs(id) on delete cascade,
    email       varchar(256) not null,
    name        varchar(128) not null,
    created_at  timestamptz  not null default now(),
    updated_at  timestamptz  not null default now(),
    deleted_at  timestamptz,
    unique (org_id, email)
);

create table roles (
    id           uuid primary key default gen_random_uuid(),
    -- org_id 为 null 表示系统内置角色(Viewer/Editor/Admin),所有组织可用
    org_id       uuid references orgs(id) on delete cascade,
    name         varchar(64) not null,
    -- 可选值集合: view / edit / create / delete / manage_permissions
    permissions  jsonb       not null default '[]',
    is_system    boolean     not null default false,
    created_at   timestamptz not null default now()
);

-- 内置角色种子数据
insert into roles (name, permissions, is_system) values
    ('Viewer', '["view"]', true),
    ('Editor', '["view","edit","create"]', true),
    ('Admin',  '["view","edit","create","delete","manage_permissions"]', true);

-- ---------------------------------------------------------------------
-- 3. 组件维度权限绑定
-- ---------------------------------------------------------------------
create table component_role_bindings (
    id            uuid primary key default gen_random_uuid(),
    component_id  uuid not null references components(id) on delete cascade,
    user_id       uuid not null references users(id) on delete cascade,
    role_id       uuid not null references roles(id),
    granted_by    uuid references users(id),
    granted_at    timestamptz not null default now(),
    unique (component_id, user_id)
);
create index idx_role_bindings_component on component_role_bindings(component_id);
create index idx_role_bindings_user on component_role_bindings(user_id);

-- ---------------------------------------------------------------------
-- 4. 配置管理: 组件参数配置(支持全局默认 + 环境级覆盖)
-- ---------------------------------------------------------------------
create table component_configs (
    id             uuid primary key default gen_random_uuid(),
    component_id   uuid not null references components(id) on delete cascade,
    -- null = 全局默认值,适用于该组件所有环境;非空 = 仅覆盖该环境
    environment_id uuid references environments(id) on delete cascade,

    key            varchar(128) not null,
    -- 非敏感值明文存储;敏感值 value 留空,值本身存在外部 secret 管理系统
    value          text,
    is_secret      boolean      not null default false,
    secret_ref     varchar(128),

    description    text,
    created_by     uuid references users(id),
    updated_by     uuid references users(id),
    created_at     timestamptz  not null default now(),
    updated_at     timestamptz  not null default now()
);
-- 全局键在组件内唯一
create unique index idx_configs_global on component_configs(component_id, key)
    where environment_id is null;
-- 环境级键在组件+环境内唯一
create unique index idx_configs_env on component_configs(component_id, environment_id, key)
    where environment_id is not null;

-- 配置变更审计
create table component_config_history (
    id             uuid primary key default gen_random_uuid(),
    component_id   uuid not null references components(id) on delete cascade,
    environment_id uuid references environments(id),
    key            varchar(128) not null,
    -- create / update / delete
    action         varchar(16)  not null,
    old_value      text,
    new_value      text,
    changed_by     uuid references users(id),
    changed_at     timestamptz  not null default now()
);
create index idx_config_history_component on component_config_history(component_id, changed_at desc);

-- ---------------------------------------------------------------------
-- 5. 流水线版本化(编辑流水线定义时快照,运行记录关联具体版本号)
-- ---------------------------------------------------------------------
alter table pipelines add column version integer not null default 1;

create table pipeline_versions (
    id           uuid primary key default gen_random_uuid(),
    pipeline_id  uuid    not null references pipelines(id) on delete cascade,
    version      integer not null,
    -- 该版本下完整的 stages + task_templates 快照(结构与 CRD 的 Tasks[] 一致)
    snapshot     jsonb   not null,
    created_by   uuid references users(id),
    created_at   timestamptz not null default now(),
    unique (pipeline_id, version)
);

alter table pipeline_runs add column pipeline_version integer;
-- 触发运行时记录当时的 pipelines.version,历史运行记录不受后续编辑影响

-- ---------------------------------------------------------------------
-- 6. 产物管理: 永久版本化的构建产物,供查看/下载
-- 区别于 Produces/Consumes(流水线内部跨阶段的临时产物传递,生命周期绑定单次运行):
-- artifacts 是归档阶段主动登记的、面向用户可见的交付物。
-- ---------------------------------------------------------------------
create table artifacts (
    id              uuid primary key default gen_random_uuid(),
    component_id    uuid not null references components(id) on delete cascade,
    pipeline_run_id uuid references pipeline_runs(id),
    task_run_id     uuid references task_runs(id), -- 通常是"归档阶段"对应的 task_run

    -- 版本标识,如 v1.2.3 或基于 commit 的构建号
    version         varchar(128) not null,
    -- image / binary / archive / generic
    artifact_type   varchar(32)  not null default 'generic',

    storage_key     varchar(512) not null, -- 对象存储路径,下载时按需生成签名 URL
    size_bytes      bigint,
    checksum        varchar(128),
    commit_sha      varchar(64),

    -- 可选的保留策略,过期后由后台任务清理存储与本行
    expires_at      timestamptz,
    created_at      timestamptz  not null default now(),
    unique (component_id, version, artifact_type)
);
create index idx_artifacts_component on artifacts(component_id, created_at desc);
