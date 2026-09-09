-- ============================================================
-- 03_clusters.sql — 集群注册表
-- 对应页面: 平台管理-集群
-- 表: clusters
-- 前缀: c1=clusters
-- 说明: status/last_heartbeat_at 正常由 runner gateway 心跳回写,
--       这里只是测试初始值。
-- ============================================================

INSERT INTO clusters (id, name, vendor, region, status, agent_version, last_heartbeat_at, created_at, updated_at) VALUES
('c1000000-0000-0000-0000-000000000001', 'kind-dev',     'kind',    'local',  'online',  'v0.0.1', now() - interval '2 minutes',  now() - interval '85 days', now() - interval '2 minutes'),
('c1000000-0000-0000-0000-000000000002', 'staging-01',   'k3s',     'cn-east','online',  'v0.0.1', now() - interval '5 minutes',  now() - interval '85 days', now() - interval '5 minutes'),
('c1000000-0000-0000-0000-000000000003', 'prod-east-1',  'kubernetes','cn-east','offline', '',      NULL,                          now() - interval '85 days', now() - interval '30 days')
ON CONFLICT (id) DO NOTHING;
