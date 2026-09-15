-- =====================================================================
-- 0005_rbac_p3.sql — P3 §7 subject 模型落地（对应 DATA-MODEL §7.3）
-- 运行: cmd/hub/conf/import.sh （在 AutoMigrate 之后亦可，语句幂等）
--
-- 把 component_role_bindings 从 V1（逐用户 user_id + roles 表 RoleID）
-- 演进为 §7（subject_type[user|group] + subject_id + component_role_id）。
--   * 新增 component_role_id（指向 component_roles §7 角色）。
--   * user_id / role_id 改为可空：§7 的组绑定没有 user，纯 §7 绑定
--     也不需要 V1 RoleID；存量 V1 行（user_id/role_id 已填）不受影响。
-- 新建的 component_roles / platform_roles / platform_role_bindings 由
-- GORM AutoMigrate 创建（见 internal/db/db.go），此处不重复建表。
-- =====================================================================

ALTER TABLE component_role_bindings
  ADD COLUMN IF NOT EXISTS component_role_id uuid;

-- 存量 V1 行 user_id/role_id 已 NOT NULL 且已填值；改可空不影响它们，
-- 仅为 §7 的组绑定 / 纯 component_role_id 绑定放开约束。
ALTER TABLE component_role_bindings
  ALTER COLUMN user_id DROP NOT NULL;

ALTER TABLE component_role_bindings
  ALTER COLUMN role_id DROP NOT NULL;
