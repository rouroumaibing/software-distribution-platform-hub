-- ============================================================
-- 09_rbac_multiorg.sql — §7 两层 RBAC 预置角色（多 org 目标态）
-- 表: platform_roles, component_roles
-- 前缀: f1=platform_roles, f2=component_roles
-- 说明:
--   * 预置角色 OrgID 为 NULL（built-in / 全局），与 V1 内置角色一致；
--     各 org 的自定义角色由运行时创建。
--   * actions 为 JSONB 数组，取值见 DATA-MODEL §7.2 / §7.3。
-- ============================================================

-- ---------- 平台级角色 ----------
INSERT INTO platform_roles (id, org_id, name, description, actions, is_system, created_at) VALUES
('f1000000-0000-0000-0000-000000000001', NULL, 'sdp-admin',
 '平台管理员：全部页面与全局动作',
 '["page:overview:view","page:servicetree:view","page:settings:view","org:manage","user:manage","component:create","environment:create"]'::jsonb,
 true, now()),
('f1000000-0000-0000-0000-000000000002', NULL, 'sdp-viewer',
 '平台只读：仅页面查看',
 '["page:overview:view","page:servicetree:view","page:settings:view"]'::jsonb,
 true, now())
ON CONFLICT (id) DO NOTHING;

-- ---------- 组件级角色（ArgoCD Project-scoped 风格） ----------
INSERT INTO component_roles (id, org_id, name, description, actions, is_system, created_at) VALUES
('f2000000-0000-0000-0000-000000000001', NULL, 'component-viewer',
 '组件只读',
 '["component:read","pipeline:read","config:read","artifact:read"]'::jsonb,
 true, now()),
('f2000000-0000-0000-0000-000000000002', NULL, 'component-editor',
 '组件编辑 + 触发流水线',
 '["component:read","component:update","pipeline:read","pipeline:create","pipeline:update","pipeline:delete","pipeline:trigger","config:read","config:update","artifact:read","artifact:download"]'::jsonb,
 true, now()),
('f2000000-0000-0000-0000-000000000003', NULL, 'component-approver',
 '组件审批人（仅 approver 持有 approval:approve）',
 '["component:read","pipeline:read","approval:approve","artifact:read"]'::jsonb,
 true, now()),
('f2000000-0000-0000-0000-000000000004', NULL, 'component-admin',
 '组件管理员：组件全部动作（创建组件的 owner 自动绑定此角色，见 §7.4）',
 '["component:read","component:update","component:delete","component:create","component:manage","pipeline:read","pipeline:create","pipeline:update","pipeline:delete","pipeline:trigger","approval:approve","config:read","config:update","artifact:read","artifact:download"]'::jsonb,
 true, now())
ON CONFLICT (id) DO NOTHING;
