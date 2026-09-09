-- ============================================================
-- 04_environments.sql — 组件环境(组件 x 集群 x namespace)
-- 对应页面: 组件详情-环境
-- 表: environments
-- 前缀: c2=environments
-- 命名约定: namespace = {org-slug}-{component-key}-{env-key}
-- ============================================================

INSERT INTO environments (id, component_id, key, name, cluster_id, env_type, namespace, created_at, updated_at) VALUES
-- order-service: dev / staging / prod 三套
('c2000000-0000-0000-0000-000000000001', 'b2000000-0000-0000-0000-000000000004', 'dev',     '开发环境', 'c1000000-0000-0000-0000-000000000001', 'test',       'platform-eng-order-service-dev',     now() - interval '70 days', now() - interval '5 days'),
('c2000000-0000-0000-0000-000000000002', 'b2000000-0000-0000-0000-000000000004', 'staging', '预发环境', 'c1000000-0000-0000-0000-000000000002', 'test',       'platform-eng-order-service-staging', now() - interval '70 days', now() - interval '5 days'),
('c2000000-0000-0000-0000-000000000003', 'b2000000-0000-0000-0000-000000000004', 'prod',    '生产环境', 'c1000000-0000-0000-0000-000000000003', 'production', 'platform-eng-order-service-prod',    now() - interval '70 days', now() - interval '5 days'),
-- api-gateway: dev / prod 两套
('c2000000-0000-0000-0000-000000000004', 'b2000000-0000-0000-0000-000000000001', 'dev',     '开发环境', 'c1000000-0000-0000-0000-000000000001', 'test',       'platform-eng-api-gateway-dev',       now() - interval '75 days', now() - interval '5 days'),
('c2000000-0000-0000-0000-000000000005', 'b2000000-0000-0000-0000-000000000001', 'prod',    '生产环境', 'c1000000-0000-0000-0000-000000000003', 'production', 'platform-eng-api-gateway-prod',      now() - interval '75 days', now() - interval '5 days'),
-- auth-service: dev / staging 两套
('c2000000-0000-0000-0000-000000000006', 'b2000000-0000-0000-0000-000000000002', 'dev',     '开发环境', 'c1000000-0000-0000-0000-000000000001', 'test',       'platform-eng-auth-service-dev',      now() - interval '75 days', now() - interval '5 days'),
('c2000000-0000-0000-0000-000000000007', 'b2000000-0000-0000-0000-000000000002', 'staging', '预发环境', 'c1000000-0000-0000-0000-000000000002', 'test',       'platform-eng-auth-service-staging',  now() - interval '75 days', now() - interval '5 days')
ON CONFLICT (id) DO NOTHING;
