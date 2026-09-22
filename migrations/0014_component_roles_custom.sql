-- =====================================================================
-- 0014_component_roles_custom.sql
--   组件级自定义角色（B-11 自定义角色）：唯一约束
--   运行: 手动执行（与 0004~0013 同路径）。语句幂等，可重复跑。
--     kubectl -n sdp-workflow exec -i deploy/postgres -- \
--       psql -v ON_ERROR_STOP=1 -U sdp -d sdp < migrations/0014_component_roles_custom.sql
--
-- 背景:
--   `component_roles` 一直存在（AutoMigrate 建表），但**只有 List** ——
--   自定义角色（`org_id is not null`）无法经 API 建立，只能靠 seed SQL。B-11
--   补上 Create/Update/Delete 之后，"同名角色"第一次可以被用户造出来，于是
--   第一次需要一个约束把名字按组织钉住。
--
-- 说明:
--   * 唯一键是 **(org_id, name)**，不是全局 name：两个组织各自定义
--     "release-owner" 是完全合理的，按全局唯一反而会互相踩。
--   * 内置角色 `org_id is null`（component-viewer / -editor / -approver /
--     -admin），它们的名字必须在**全局**唯一 —— 因为 `GetByName` 是按名字
--     解析内置角色的（owner 引导 / 绑定校验都走它），两条同名内置角色会让
--     解析结果二义。所以拆成两条 partial unique index。
--   * partial（`where ... is not null` / `is null`）而不是复合 unique：Postgres
--     的 unique 里 NULL 互不相等，`unique (org_id, name)` 对内置角色**完全不起
--     作用**（多行 `(null, 'component-admin')` 都能插入）。这正是 pipelines /
--     pipeline_stages 之前踩过的坑（见 plan §3.1「uniqueIndex 标签写漏」）。
--   * 表无软删列（模型无 `deleted_at`），删除是硬删，故不需要
--     `where deleted_at is null`。
--
-- ⚠️ 若第 1/2 步报 duplicate key:
--   说明库里已有重名行（历史 seed / 手工写入）。**先查再改**:
--     select coalesce(org_id::text, '<内置>'), name, count(*) from component_roles
--      group by 1, 2 having count(*) > 1;
--   人工判重（保留哪一个、改名还是删掉）后再跑本文件。
-- =====================================================================

-- 1) 自定义角色：同一组织内名字唯一
create unique index if not exists idx_component_roles_org_name
    on component_roles (org_id, name)
    where org_id is not null;

-- 2) 内置角色：名字全局唯一（GetByName 按名字解析内置角色，不能二义）
create unique index if not exists idx_component_roles_builtin_name
    on component_roles (name)
    where org_id is null;
