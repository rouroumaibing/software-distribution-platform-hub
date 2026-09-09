-- =====================================================================
-- Hub 数据库表结构
-- 覆盖层级: 组织 -> 服务树 -> 服务 -> 组件 -> 流水线 -> 阶段 -> 子任务模板
-- 以及: 环境/集群注册、运行记录(落库自 K8s CR)、审批记录
-- 约定: 所有主键用 UUID,所有表都有 created_at/updated_at,软删除用 deleted_at
-- =====================================================================

create extension if not exists pgcrypto; -- for gen_random_uuid()

-- ---------------------------------------------------------------------
-- 1. 组织(租户根节点)
-- ---------------------------------------------------------------------
create table orgs (
    id          uuid primary key default gen_random_uuid(),
    name        varchar(128) not null,
    slug        varchar(64)  not null unique, -- 用于 URL / namespace 命名
    created_at  timestamptz  not null default now(),
    updated_at  timestamptz  not null default now(),
    deleted_at  timestamptz
);

-- ---------------------------------------------------------------------
-- 2. 服务树(每个 org 一棵,存在感很低,主要用于承载根节点元信息)
-- ---------------------------------------------------------------------
create table service_trees (
    id          uuid primary key default gen_random_uuid(),
    org_id      uuid not null unique references orgs(id) on delete cascade,
    name        varchar(128) not null default 'default',
    created_at  timestamptz  not null default now(),
    updated_at  timestamptz  not null default now()
);

-- ---------------------------------------------------------------------
-- 3. 服务(业务服务单元,比如"交易服务"、"用户中心")
-- ---------------------------------------------------------------------
create table services (
    id               uuid primary key default gen_random_uuid(),
    service_tree_id  uuid not null references service_trees(id) on delete cascade,
    key              varchar(64)  not null, -- URL/slug,树内唯一
    name             varchar(128) not null,
    description      text,
    owner_team       varchar(128),
    created_at       timestamptz  not null default now(),
    updated_at       timestamptz  not null default now(),
    deleted_at       timestamptz,
    unique (service_tree_id, key)
);
create index idx_services_tree on services(service_tree_id) where deleted_at is null;

-- ---------------------------------------------------------------------
-- 4. 组件(可独立部署单元,通常对应一个代码仓库)
-- ---------------------------------------------------------------------
create table components (
    id              uuid primary key default gen_random_uuid(),
    service_id      uuid not null references services(id) on delete cascade,
    key             varchar(64)  not null, -- 服务内唯一
    name            varchar(128) not null,
    repo_url        varchar(512) not null,
    default_branch  varchar(128) not null default 'main',
    -- 私有仓库凭据不落库明文,存引用;真正的 secret 由 Runner 侧 K8s Secret 持有
    repo_secret_ref varchar(128),
    language        varchar(64),
    description     text,
    created_at      timestamptz  not null default now(),
    updated_at      timestamptz  not null default now(),
    deleted_at      timestamptz,
    unique (service_id, key)
);
create index idx_components_service on components(service_id) where deleted_at is null;

-- ---------------------------------------------------------------------
-- 5. 集群注册(前面 Hub-Spoke 架构讨论过的 cluster registry)
-- ---------------------------------------------------------------------
create table clusters (
    id             uuid primary key default gen_random_uuid(),
    name           varchar(128) not null unique,
    vendor         varchar(64)  not null, -- aliyun / tencent / aws / self-hosted ...
    region         varchar(64)  not null,
    status         varchar(32)  not null default 'offline', -- online / offline
    agent_version  varchar(32),
    last_heartbeat_at timestamptz,
    created_at     timestamptz  not null default now(),
    updated_at     timestamptz  not null default now()
);

-- ---------------------------------------------------------------------
-- 6. 环境(挂在组件下,如 beta/alpha/gamma/prod,绑定到具体集群+命名空间)
-- ---------------------------------------------------------------------
create table environments (
    id            uuid primary key default gen_random_uuid(),
    component_id  uuid not null references components(id) on delete cascade,
    key           varchar(64)  not null, -- beta / alpha / gamma / prod
    name          varchar(128) not null,
    cluster_id    uuid not null references clusters(id),
    namespace     varchar(128) not null, -- 约定: {org}-{component}-{env}
    created_at    timestamptz  not null default now(),
    updated_at    timestamptz  not null default now(),
    unique (component_id, key)
);
create index idx_environments_component on environments(component_id);
create index idx_environments_cluster on environments(cluster_id);

-- ---------------------------------------------------------------------
-- 7. 流水线(定义级,如"构建流水线"、"发布流水线")
-- ---------------------------------------------------------------------
create table pipelines (
    id            uuid primary key default gen_random_uuid(),
    component_id  uuid not null references components(id) on delete cascade,
    name          varchar(128) not null,
    -- build: 构建类   release: 发布类   custom: 自定义
    kind          varchar(32)  not null default 'custom',
    description   text,
    created_by    varchar(128),
    created_at    timestamptz  not null default now(),
    updated_at    timestamptz  not null default now(),
    deleted_at    timestamptz,
    unique (component_id, name)
);
create index idx_pipelines_component on pipelines(component_id) where deleted_at is null;

-- ---------------------------------------------------------------------
-- 8. 流水线阶段(定义级,阶段顺序执行,阶段内子任务并行)
-- ---------------------------------------------------------------------
create table pipeline_stages (
    id           uuid primary key default gen_random_uuid(),
    pipeline_id  uuid not null references pipelines(id) on delete cascade,
    name         varchar(128) not null, -- 构建阶段 / 归档阶段 / 审批阶段 / 部署阶段 / 测试阶段
    sequence     integer      not null, -- 执行顺序,越小越先执行
    created_at   timestamptz  not null default now(),
    updated_at   timestamptz  not null default now(),
    unique (pipeline_id, sequence)
);
create index idx_stages_pipeline on pipeline_stages(pipeline_id);

-- ---------------------------------------------------------------------
-- 9. 子任务模板(阶段内的具体任务定义,对应 CRD 里的 PipelineTaskSpec 模板)
-- ---------------------------------------------------------------------
create table pipeline_task_templates (
    id                uuid primary key default gen_random_uuid(),
    stage_id          uuid not null references pipeline_stages(id) on delete cascade,
    name              varchar(128) not null, -- 构建 / 归档 / 审批人审批 / 部署beta / 自动化测试
    -- Normal / Approval / Deploy,对应 CRD 的 PipelineTaskType
    type              varchar(32)  not null,
    display_order     integer      not null default 0, -- 阶段内展示顺序(执行仍是并行)

    -- 脚本化执行相关(Normal / Deploy 任务)
    image             varchar(256),
    script_path       varchar(256), -- build.sh / run.sh / test.sh
    script_args       jsonb        not null default '[]', -- ["beta"]
    produces          jsonb        not null default '[]', -- 产物 key 列表
    consumes          jsonb        not null default '[]', -- 依赖的上游产物 key

    -- Deploy 任务专属:目标环境 + 灰度策略模板
    environment_id    uuid references environments(id),
    rollout_config    jsonb, -- CanaryStep 步进策略、健康检查配置的 JSON 快照

    -- Approval 任务专属
    approval_config   jsonb, -- {requiredApprovals, allowedApprovers, timeoutSeconds}

    retry_policy      jsonb        not null default '{"maxRetries":0}',
    timeout_seconds    integer     not null default 0,

    created_at        timestamptz  not null default now(),
    updated_at        timestamptz  not null default now(),
    unique (stage_id, name)
);
create index idx_task_templates_stage on pipeline_task_templates(stage_id);
create index idx_task_templates_environment on pipeline_task_templates(environment_id);

-- =====================================================================
-- 运行记录(从 K8s CR 落库持久化,CR 本身只保留近期用于 Reconcile 和调试)
-- =====================================================================

-- ---------------------------------------------------------------------
-- 10. 流水线运行记录
-- ---------------------------------------------------------------------
create table pipeline_runs (
    id                  uuid primary key default gen_random_uuid(),
    pipeline_id         uuid not null references pipelines(id),
    cluster_id          uuid not null references clusters(id),
    -- 对应 K8s 里的 PipelineRun CR 名称,近期数据可据此反查实时状态
    cr_name             varchar(256) not null,
    cr_namespace        varchar(128) not null,

    commit_sha          varchar(64),
    params              jsonb        not null default '{}',

    -- Pending / Running / WaitingApproval / Succeeded / Failed / Cancelled
    phase               varchar(32)  not null default 'Pending',
    triggered_by        varchar(128),

    start_time          timestamptz,
    completion_time     timestamptz,
    message             text,

    created_at          timestamptz  not null default now(),
    updated_at          timestamptz  not null default now()
);
create index idx_pipeline_runs_pipeline on pipeline_runs(pipeline_id, created_at desc);
create index idx_pipeline_runs_phase on pipeline_runs(phase);
create unique index idx_pipeline_runs_cr on pipeline_runs(cluster_id, cr_namespace, cr_name);

-- ---------------------------------------------------------------------
-- 11. 任务运行记录(DAG 中每个子任务节点的执行历史)
-- ---------------------------------------------------------------------
create table task_runs (
    id                  uuid primary key default gen_random_uuid(),
    pipeline_run_id     uuid not null references pipeline_runs(id) on delete cascade,
    task_template_id    uuid references pipeline_task_templates(id),
    cr_name             varchar(256) not null, -- 对应 K8s TaskRun CR 名称

    task_name           varchar(128) not null,
    stage_name          varchar(128) not null,
    type                varchar(32)  not null, -- Normal / Approval / Deploy

    -- Pending / Running / Succeeded / Failed / Skipped
    phase               varchar(32)  not null default 'Pending',
    retry_count         integer      not null default 0,
    exit_code           integer,

    start_time          timestamptz,
    completion_time     timestamptz,
    logs_ref            varchar(512), -- 归档后的日志对象存储 key
    message             text,

    created_at          timestamptz  not null default now(),
    updated_at          timestamptz  not null default now()
);
create index idx_task_runs_pipeline_run on task_runs(pipeline_run_id);
create index idx_task_runs_stage on task_runs(pipeline_run_id, stage_name);

-- ---------------------------------------------------------------------
-- 12. 审批记录(独立表便于审计查询,一个 Approval 任务可有多条审批意见)
-- ---------------------------------------------------------------------
create table approvals (
    id           uuid primary key default gen_random_uuid(),
    task_run_id  uuid not null references task_runs(id) on delete cascade,
    approver     varchar(128) not null,
    -- approved / rejected
    decision     varchar(16)  not null,
    comment      text,
    decided_at   timestamptz  not null default now()
);
create index idx_approvals_task_run on approvals(task_run_id);

-- ---------------------------------------------------------------------
-- 13. 灰度发布记录(Deploy 任务对应的 Rollout 执行历史,便于回看权重变化)
-- ---------------------------------------------------------------------
create table rollout_runs (
    id                   uuid primary key default gen_random_uuid(),
    task_run_id          uuid not null references task_runs(id) on delete cascade,
    workload_ref         varchar(256) not null,

    -- Progressing / Paused / Healthy / Degraded / RollingBack
    phase                varchar(32)  not null default 'Progressing',
    current_step_index   integer      not null default 0,
    current_weight       integer      not null default 0,

    start_time           timestamptz,
    completion_time      timestamptz,

    created_at           timestamptz  not null default now(),
    updated_at           timestamptz  not null default now()
);
create index idx_rollout_runs_task_run on rollout_runs(task_run_id);
