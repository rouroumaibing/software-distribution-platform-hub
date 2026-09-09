-- ============================================================
-- 07_component_configs.sql — 组件配置参数
-- 对应页面: 组件详情-配置
-- 表: component_configs
-- 前缀: f1=component_configs
-- 说明: environment_id 为 NULL 表示全局默认值, 非 NULL 表示
--       覆盖该环境的值(合并逻辑见 ComponentConfig 模型注释)。
-- ============================================================

INSERT INTO component_configs (id, component_id, environment_id, key, value, is_secret, secret_ref, description, created_by, updated_by, created_at, updated_at) VALUES
-- order-service: 全局默认
('f1000000-0000-0000-0000-000000000001', 'b2000000-0000-0000-0000-000000000004', NULL, 'replicas',   '3', false, '',           '副本数', NULL, NULL, now() - interval '30 days', now() - interval '30 days'),
('f1000000-0000-0000-0000-000000000002', 'b2000000-0000-0000-0000-000000000004', NULL, 'db.host',    'postgres.sdp-system.svc', false, '', '订单库地址', NULL, NULL, now() - interval '30 days', now() - interval '30 days'),
('f1000000-0000-0000-0000-000000000003', 'b2000000-0000-0000-0000-000000000004', NULL, 'db.password','', true, 'secret/order-db', '订单库密码(引用 K8s Secret)', NULL, NULL, now() - interval '30 days', now() - interval '30 days'),
-- order-service: dev 环境覆盖
('f1000000-0000-0000-0000-000000000004', 'b2000000-0000-0000-0000-000000000004', 'c2000000-0000-0000-0000-000000000001', 'replicas', '1', false, '', '开发环境只跑 1 副本', NULL, NULL, now() - interval '20 days', now() - interval '20 days'),
-- api-gateway: 全局默认
('f1000000-0000-0000-0000-000000000005', 'b2000000-0000-0000-0000-000000000001', NULL, 'replicas',      '2', false, '', '网关副本数', NULL, NULL, now() - interval '28 days', now() - interval '28 days'),
('f1000000-0000-0000-0000-000000000006', 'b2000000-0000-0000-0000-000000000001', NULL, 'rateLimit.qps', '1000', false, '', '限流 QPS', NULL, NULL, now() - interval '28 days', now() - interval '28 days')
ON CONFLICT (id) DO NOTHING;
