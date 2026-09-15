-- =====================================================================
-- 0004_rbac_multiorg.sql — 多 org RBAC 演进（对应 DATA-MODEL §7 目标态）
-- 运行: cmd/hub/conf/import.sh （在 AutoMigrate 之后亦可，语句幂等）
--
-- 本文件只做 AutoMigrate 做不到的两件事：
--   1) 给已有的 V1 表 component_role_bindings 补多 org 维度 (org_id)
--      并回填 subject_type/subject_id（V1 是逐用户的 user_id）。
--   2) 给 components 加所有权列 owner_user / owner_group（§7.4）。
--
-- 新建的 4 张表 (platform_roles / platform_role_bindings /
-- component_roles / pipeline_approvals) 由 GORM AutoMigrate 创建
-- （见 internal/db/db.go），这里不再重复建表，避免类型漂移。
-- =====================================================================

-- ---------- 1. component_role_bindings 多 org 维度 ----------
ALTER TABLE component_role_bindings
  ADD COLUMN IF NOT EXISTS org_id uuid;

ALTER TABLE component_role_bindings
  ADD COLUMN IF NOT EXISTS subject_type varchar(16);

ALTER TABLE component_role_bindings
  ADD COLUMN IF NOT EXISTS subject_id varchar(128);

-- 回填：component → service → service_tree → org
-- V1 绑定全部是 user 主体，故 subject_type 统一为 'user'。
UPDATE component_role_bindings crb
SET org_id       = st.org_id,
    subject_type = 'user',
    subject_id   = crb.user_id::text
FROM components c
JOIN services s       ON s.id = c.service_id
JOIN service_trees st ON st.id = s.service_tree_id
WHERE crb.component_id = c.id
  AND crb.org_id IS NULL;

-- 注：org_id 在模型里保持可空（*uuid.UUID），故此处不强制 NOT NULL，
-- 避免存量孤儿行导致迁移中断；RBAC 查询按 org_id 过滤即可隔离。

-- ---------- 2. components 所有权列（§7.4 默认审批人来源） ----------
ALTER TABLE components
  ADD COLUMN IF NOT EXISTS owner_user uuid;

ALTER TABLE components
  ADD COLUMN IF NOT EXISTS owner_group varchar(128);
