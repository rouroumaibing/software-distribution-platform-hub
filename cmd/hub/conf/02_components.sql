-- ============================================================
-- 02_components.sql — 组件(可独立部署单元)
-- 对应页面: 服务树(点开 Service 后的组件列表)、组件详情-概览
-- 表: components
-- 前缀: b2=components
-- ============================================================

INSERT INTO components (id, service_id, key, name, repo_url, default_branch, repo_secret_ref, language, description, created_at, updated_at) VALUES
('b2000000-0000-0000-0000-000000000001', 'b1000000-0000-0000-0000-000000000001', 'api-gateway',  'API 网关',   'https://git.example.com/platform-eng/api-gateway.git',  'main', 'git-cred', 'Go',      'HTTP/gRPC 统一接入网关, 路由、限流、鉴权', now() - interval '78 days', now() - interval '5 days'),
('b2000000-0000-0000-0000-000000000002', 'b1000000-0000-0000-0000-000000000002', 'auth-service', '认证服务',   'https://git.example.com/platform-eng/auth-service.git', 'main', 'git-cred', 'Go',      'OIDC/OAuth2 认证授权服务',                 now() - interval '78 days', now() - interval '5 days'),
('b2000000-0000-0000-0000-000000000003', 'b1000000-0000-0000-0000-000000000002', 'user-web',     '用户中心前端','https://git.example.com/platform-eng/user-web.git',    'main', 'git-cred', 'TypeScript','用户中心 Web 前端(Vue3 + Vite)',           now() - interval '70 days', now() - interval '5 days'),
('b2000000-0000-0000-0000-000000000004', 'b1000000-0000-0000-0000-000000000003', 'order-service','订单服务',   'https://git.example.com/platform-eng/order-service.git','main', 'git-cred', 'Go',      '订单创建、查询、状态机管理',               now() - interval '72 days', now() - interval '3 days'),
('b2000000-0000-0000-0000-000000000005', 'b1000000-0000-0000-0000-000000000003', 'payment-service','支付服务', 'https://git.example.com/platform-eng/payment-service.git','main','git-cred', 'Go',      '支付渠道对接与对账',                       now() - interval '72 days', now() - interval '3 days'),
('b2000000-0000-0000-0000-000000000006', 'b1000000-0000-0000-0000-000000000004', 'push-server',  '推送服务',   'https://git.example.com/platform-eng/push-server.git',  'main', 'git-cred', 'Go',      '站内信与推送消息下发',                     now() - interval '55 days', now() - interval '12 days'),
('b2000000-0000-0000-0000-000000000007', 'b1000000-0000-0000-0000-000000000005', 'metrics-agent','指标采集器', 'https://git.example.com/platform-eng/metrics-agent.git','main', 'git-cred', 'Go',      '主机与容器指标采集 Agent',                 now() - interval '45 days', now() - interval '18 days')
ON CONFLICT (id) DO NOTHING;
