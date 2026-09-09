-- ============================================================
-- 00_orgs.sql — 组织 + 服务树根
-- 对应页面: 总览(Dashboard)、平台管理
-- 表: orgs, service_trees
-- 约定: 所有 ID 使用确定性 UUID(前缀区分实体类型), 供后续
--       文件外键引用; ON CONFLICT DO NOTHING 保证可重复导入。
-- 前缀: a1=orgs, a2=service_trees
-- ============================================================

-- 组织: 平台工程部(中性示例, 不与 hub 读时自举的 Default/default 冲突)
INSERT INTO orgs (id, name, slug, created_at, updated_at) VALUES
('a1000000-0000-0000-0000-000000000001', '平台工程部', 'platform-eng',
 now() - interval '90 days', now() - interval '90 days')
ON CONFLICT (id) DO NOTHING;

-- 组织 1:1 服务树根(新模型中 service_trees 只有一行根记录,
-- 业务分组挂在 services 表, 见 01_services.sql)
INSERT INTO service_trees (id, org_id, name, created_at, updated_at) VALUES
('a2000000-0000-0000-0000-000000000001', 'a1000000-0000-0000-0000-000000000001', 'platform-eng-tree',
 now() - interval '90 days', now() - interval '90 days')
ON CONFLICT (id) DO NOTHING;
