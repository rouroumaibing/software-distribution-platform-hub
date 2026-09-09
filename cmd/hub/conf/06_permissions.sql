-- ============================================================
-- 06_permissions.sql — 用户 / 角色 / 组件角色绑定
-- 对应页面: 平台管理-用户与平台权限、组件详情-权限
-- 表: users, roles, component_role_bindings
-- 前缀: e1=users, e2=roles, e3=component_role_bindings
-- 说明:
--   * 系统内置角色 OrgID 为 NULL、is_system=true; auth 中间件按
--     keycloak_id(token sub)查用户, 不按 email。
--   * permissions 为 JSONB 数组, 取值见 internal/permission/models/role.go:
--     view / edit / create / delete / manage_permissions
-- ============================================================

-- ---------- 用户(挂组织) ----------
INSERT INTO users (id, org_id, email, name, keycloak_id, created_at, updated_at) VALUES
('e1000000-0000-0000-0000-000000000001', 'a1000000-0000-0000-0000-000000000001', 'admin@platform-eng.local',    'admin',    'dev-keycloak-sub-admin',    now() - interval '90 days', now() - interval '90 days'),
('e1000000-0000-0000-0000-000000000002', 'a1000000-0000-0000-0000-000000000001', 'zhang.san@platform-eng.local','zhang.san','dev-keycloak-sub-zhangsan', now() - interval '60 days', now() - interval '60 days'),
('e1000000-0000-0000-0000-000000000003', 'a1000000-0000-0000-0000-000000000001', 'li.si@platform-eng.local',    'li.si',    'dev-keycloak-sub-lisi',     now() - interval '55 days', now() - interval '55 days')
ON CONFLICT (id) DO NOTHING;

-- ---------- 角色(内置 + 自定义) ----------
INSERT INTO roles (id, org_id, name, permissions, is_system, created_at) VALUES
('e2000000-0000-0000-0000-000000000001', NULL, 'Viewer', '["view"]'::jsonb, true, now() - interval '90 days'),
('e2000000-0000-0000-0000-000000000002', NULL, 'Editor', '["view","edit","create"]'::jsonb, true, now() - interval '90 days'),
('e2000000-0000-0000-0000-000000000003', NULL, 'Admin', '["view","edit","create","delete","manage_permissions"]'::jsonb, true, now() - interval '90 days'),
('e2000000-0000-0000-0000-000000000004', 'a1000000-0000-0000-0000-000000000001', '审批人', '["view","edit"]'::jsonb, false, now() - interval '40 days')
ON CONFLICT (id) DO NOTHING;

-- ---------- 组件级角色绑定(组件详情-权限页) ----------
INSERT INTO component_role_bindings (id, component_id, user_id, role_id, granted_by, granted_at) VALUES
('e3000000-0000-0000-0000-000000000001', 'b2000000-0000-0000-0000-000000000004', 'e1000000-0000-0000-0000-000000000001', 'e2000000-0000-0000-0000-000000000003', 'e1000000-0000-0000-0000-000000000001', now() - interval '70 days'),
('e3000000-0000-0000-0000-000000000002', 'b2000000-0000-0000-0000-000000000004', 'e1000000-0000-0000-0000-000000000002', 'e2000000-0000-0000-0000-000000000002', 'e1000000-0000-0000-0000-000000000001', now() - interval '60 days'),
('e3000000-0000-0000-0000-000000000003', 'b2000000-0000-0000-0000-000000000001', 'e1000000-0000-0000-0000-000000000003', 'e2000000-0000-0000-0000-000000000001', 'e1000000-0000-0000-0000-000000000001', now() - interval '55 days'),
('e3000000-0000-0000-0000-000000000004', 'b2000000-0000-0000-0000-000000000002', 'e1000000-0000-0000-0000-000000000002', 'e2000000-0000-0000-0000-000000000004', 'e1000000-0000-0000-0000-000000000001', now() - interval '50 days')
ON CONFLICT (id) DO NOTHING;
