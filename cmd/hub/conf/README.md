# hub 测试数据 SQL(cmd/hub/conf)

页面演示/联调用的种子数据，**导入数据库使用**，不写死在代码里。
表结构以 `internal/db/db.go` 的 AutoMigrate 为准（PostgreSQL 16）。

> **D3 之后没有 `users` 表了**（`hub/ACCOUNT-PERMISSION-MODEL.md` §2.2）：hub 对身份
> 无状态，主体一律取 token 的 `sub`。种子里的绑定因此直接写 `sub` 字符串，
> 不再插入本地用户行（见 06_permissions.sql 的说明）。

## 与 old service_default_data.sql 的差异

对照 old `cmd/go-devops/conf/service_default_data.sql`（MySQL 方言：
`UUID()`、`@var` 会话变量、组件树 4 层 category/service 混排）：

| 维度 | old (MySQL) | 新 conf (PostgreSQL) |
|---|---|---|
| 主键生成 | `UUID()` | 确定性 UUID 字面量 + `ON CONFLICT (id) DO NOTHING`（跨文件外键可直引，幂等可重导） |
| 中间变量 | `SET @x = (SELECT ...)` | 不需要——ID 全部预先固定 |
| 树结构 | service_tree 单表自联 4 层 | orgs → service_trees(1:1 根) → services(业务分组) → components(组件) |
| 方言 | MySQL | PostgreSQL（`jsonb`、`now() - interval`） |

## 文件 → 页面对照

| 文件 | 表 | 覆盖页面 |
|---|---|---|
| 00_orgs.sql | orgs, service_trees | 总览、平台管理（组织） |
| 01_services.sql | services | 服务树（业务分组层） |
| 02_components.sql | components | 服务树、组件详情-概览 |
| 03_targets.sql | targets | 接入管理 |
| 04_environments.sql | environments | 组件详情-环境 |
| 05_pipelines.sql | pipelines, pipeline_stages, pipeline_task_templates, pipeline_versions | 组件详情-流水线、流水线编辑器 |
| 06_permissions.sql | roles(V1 只读), component_role_bindings | 平台管理-主体与平台权限、组件详情-权限 |
| 07_component_configs.sql | component_configs | 组件详情-配置 |
| 08_runs_artifacts.sql | pipeline_runs, task_runs, artifacts | 运行中心、组件详情-运行记录/发布/制品库/日志 |
| 09_rbac_multiorg.sql | platform_roles, platform_role_bindings, component_roles | 平台权限、组件角色选择器（§7 两层 RBAC） |

## 导入

```bash
# 方式一：脚本一键导入（按文件名顺序，幂等）
cmd/hub/conf/import.sh

# 方式二：手动单文件
kubectl -n sdp-workflow exec -i deploy/postgres -- \
  psql -v ON_ERROR_STOP=1 -U sdp -d sdp < cmd/hub/conf/00_orgs.sql
```

导入后在页面切换到「平台工程部 (platform-eng)」组织即可看到数据。
hub 的读时自举默认组织（Default/default）与本种子数据互不影响。

> **顺序注意**：种子假定表结构已是最新。删列/删表的迁移必须**先**手工执行
> （见 `migrations/README`（若有）与 `migrations/0015_d3_drop_users_and_legacy_columns.sql`），
> 否则 06 会因为列已不存在而失败。

## ID 前缀约定（确定性 UUID）

`a1`=orgs `a2`=service_trees `b1`=services `b2`=components
`c1`=targets `c2`=environments `d1`=pipelines `d2`=pipeline_stages
`d3`=pipeline_task_templates `d4`=pipeline_versions
`e1`=**已退役**（原 users，D3 删表） `e2`=roles `e3`=component_role_bindings
`f1`=component_configs `f2`=pipeline_runs `f3`=task_runs `f4`=artifacts

新增文件请沿用该前缀段，跨文件外键直接引用对应 UUID。

> ⚠️ **已知遗留**：`09_rbac_multiorg.sql`（后加）占用了 `f1`/`f2` 作为
> `platform_roles`/`component_roles` 的前缀，与本表里的 `f1`=component_configs、
> `f2`=pipeline_runs **撞名**。因为主键按表唯一、种子之间也没有 FK，功能上无害，
> 但读种子时容易串台。新文件请从 `f5` 起用，或另开 `g*` 段。

## 清空重来

种子数据无外键约束（GORM 未建 FK），按 08→00 倒序 TRUNCATE 即可：

```bash
kubectl -n sdp-workflow exec -it deploy/postgres -- psql -U sdp -d sdp \
  -c "TRUNCATE artifacts, task_runs, pipeline_runs, component_configs,
      component_role_bindings, roles, pipeline_versions,
      pipeline_task_templates, pipeline_stages, pipelines,
      environments, targets, components, services,
      service_trees, orgs CASCADE;"
```

注意：run 历史表（pipeline_runs/task_runs/artifacts）是长期数据源，
确认是纯种子数据后再清。
