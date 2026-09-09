-- ============================================================
-- 01_services.sql — 业务服务分组(服务树的二级节点)
-- 对应页面: 服务树(ServiceTreeView)
-- 表: services
-- 前缀: b1=services
-- 说明: 新模型中 Service 挂在 service_tree_id 下(扁平一层),
--       Component 再挂到具体 Service 上, 见 02_components.sql。
-- ============================================================

INSERT INTO services (id, service_tree_id, key, name, description, owner_team, created_at, updated_at) VALUES
('b1000000-0000-0000-0000-000000000001', 'a2000000-0000-0000-0000-000000000001', 'gateway',       '网关服务', '统一接入层, 流量路由与鉴权', 'platform-eng', now() - interval '80 days', now() - interval '10 days'),
('b1000000-0000-0000-0000-000000000002', 'a2000000-0000-0000-0000-000000000001', 'user-center',   '用户中心', '用户注册、登录、个人资料',   'platform-eng', now() - interval '80 days', now() - interval '10 days'),
('b1000000-0000-0000-0000-000000000003', 'a2000000-0000-0000-0000-000000000001', 'trade',         '交易服务', '订单、支付等交易域服务',     'platform-eng', now() - interval '75 days', now() - interval '8 days'),
('b1000000-0000-0000-0000-000000000004', 'a2000000-0000-0000-0000-000000000001', 'push',          '消息推送', '站内信、短信、推送通道',     'platform-eng', now() - interval '60 days', now() - interval '15 days'),
('b1000000-0000-0000-0000-000000000005', 'a2000000-0000-0000-0000-000000000001', 'observability', '监控告警', '指标采集、日志、告警通知',   'platform-eng', now() - interval '50 days', now() - interval '20 days')
ON CONFLICT (id) DO NOTHING;
