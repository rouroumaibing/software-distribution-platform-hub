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

-- ---------- 平台级绑定（C-10 / ACCOUNT-PERMISSION-MODEL §10 #15 的可达性种子） ----------
-- 为什么必须种：D2①（hub 表是唯一权限权威）落地 + 打开鉴权后，若一条平台级绑定
-- 都没有，就会出现「鉴权已开、人确实是管理员、但没有任何主体持有平台级权限」
-- —— 连配置权限的入口都进不去（plans/ACCOUNT-PERMISSION-DECISIONS.md §2.5）。
--
-- 为什么用**组**而不是用户：用户绑定的主体是 Keycloak `sub`，运行时才生成，
-- 无法写进静态 SQL；组主体与 sub 无关，可随本文件导入。
--
-- 前置（realm 侧，已随 realm JSON 补齐）：realm `sdp` 现在含顶层组 `/sdp-admin`，
--   且预置用户 `admin` 已加入它（见
--   build/hub/charts/software-distribution-platform-hub/templates/keycloak-realm-configmap.yaml
--   的顶层 `groups` 与 `users[admin].groups`）。`sdp-console` 的 groups mapper 已挂
--   且 `full.path=true`，故 claim 里的值是 **`/sdp-admin`**（带前导斜杠，§5.3）。
--   ⚠️ `--import-realm` **不覆盖已存在的 realm**：既有安装需手工在 Keycloak 里建组
--   并加人（或删 realm 重装），否则本绑定解析不到主体 —— 表现为开启鉴权后平台级
--   路由恒 403（ACCOUNT-PERMISSION-MODEL §5.3「装了没数据」）。
--
-- 主体格式按 §5.3 写死：`subject_type='group'` 时 subject_id 必须与 token claim
-- **逐字一致**；本 realm 挂的是 `full.path=true`，故**带前导斜杠**。
-- 注意 `/org:` 是保留前缀（组织维度，不得作为 RBAC 主体）—— 本行不是该前缀。
INSERT INTO platform_role_bindings (id, org_id, subject_type, subject_id, platform_role_id, expires_at, created_at) VALUES
('f3000000-0000-0000-0000-000000000001', NULL, 'group', '/sdp-admin',
 'f1000000-0000-0000-0000-000000000001'::uuid, NULL, now())
ON CONFLICT (id) DO NOTHING;
