-- ============================================================
-- 06_permissions.sql — 角色 / 组件角色绑定（D3 之后：没有 users 表了）
-- 对应页面: 平台管理-主体与平台权限、组件详情-权限
-- 表: roles(V1，只读)、component_role_bindings
-- 前缀: e1=**已退役**(原 users) e2=roles e3=component_role_bindings
-- 说明:
--   * D3 之后 hub **不存用户表**（ACCOUNT-PERMISSION-MODEL §2.2），所以这里不再
--     有 users 种子。绑定主体直接写 token 的 `sub`（§5.3：`subject_type='user'`
--     时 subject_id 就是 `sub` 本身），演示值沿用 Keycloak 侧约定的
--     `dev-keycloak-sub-*`。
--   * 绑定一律走 §7 路径：subject_type / subject_id + component_role_id，
--     角色指向 09_rbac_multiorg.sql 里的 component_roles。
--     V1 的 `user_id` / `role_id` 两列已随 migrations/0015 删除。
--   * V1 `roles` 表本批**保留**（`GET /roles` 仍可读），但它已不可被绑定 ——
--     没有任何列再引用它。真正的组件级角色是 component_roles。
-- ============================================================

-- ---------- V1 角色（仅历史展示；不再作为授权来源） ----------
INSERT INTO roles (id, org_id, name, permissions, is_system, created_at) VALUES
('e2000000-0000-0000-0000-000000000001', NULL, 'Viewer', '["view"]'::jsonb, true, now() - interval '90 days'),
('e2000000-0000-0000-0000-000000000002', NULL, 'Editor', '["view","edit","create"]'::jsonb, true, now() - interval '90 days'),
('e2000000-0000-0000-0000-000000000003', NULL, 'Admin', '["view","edit","create","delete","manage_permissions"]'::jsonb, true, now() - interval '90 days'),
('e2000000-0000-0000-0000-000000000004', 'a1000000-0000-0000-0000-000000000001', '审批人', '["view","edit"]'::jsonb, false, now() - interval '40 days')
ON CONFLICT (id) DO NOTHING;

-- ---------- 组件级绑定（§7：主体 = token `sub`，角色 = component_roles） ----------
-- 演示主体（与 realm 预置账号一一对应）:
--   dev-keycloak-sub-admin     → admin
--   dev-keycloak-sub-zhangsan  → zhang.san
--   dev-keycloak-sub-lisi      → li.si
-- 角色映射（V1 角色名 → §7 角色 id，见 09_rbac_multiorg.sql）:
--   Viewer → component-viewer(f2…0001) · Editor → component-editor(f2…0002)
--   审批人 → component-approver(f2…0003) · Admin → component-admin(f2…0004)
-- org_id 按 §7 的去规范化约定直接写死（本套种子只有一个组织 platform-eng）。
INSERT INTO component_role_bindings
  (id, component_id, org_id, subject_type, subject_id, component_role_id, granted_by, granted_at) VALUES
('e3000000-0000-0000-0000-000000000001', 'b2000000-0000-0000-0000-000000000004', 'a1000000-0000-0000-0000-000000000001',
 'user', 'dev-keycloak-sub-admin',    'f2000000-0000-0000-0000-000000000004', 'dev-keycloak-sub-admin', now() - interval '70 days'),
('e3000000-0000-0000-0000-000000000002', 'b2000000-0000-0000-0000-000000000004', 'a1000000-0000-0000-0000-000000000001',
 'user', 'dev-keycloak-sub-zhangsan', 'f2000000-0000-0000-0000-000000000002', 'dev-keycloak-sub-admin', now() - interval '60 days'),
('e3000000-0000-0000-0000-000000000003', 'b2000000-0000-0000-0000-000000000001', 'a1000000-0000-0000-0000-000000000001',
 'user', 'dev-keycloak-sub-lisi',     'f2000000-0000-0000-0000-000000000001', 'dev-keycloak-sub-admin', now() - interval '55 days'),
('e3000000-0000-0000-0000-000000000004', 'b2000000-0000-0000-0000-000000000002', 'a1000000-0000-0000-0000-000000000001',
 'user', 'dev-keycloak-sub-zhangsan', 'f2000000-0000-0000-0000-000000000003', 'dev-keycloak-sub-admin', now() - interval '50 days')
ON CONFLICT (id) DO NOTHING;
