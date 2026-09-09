-- ============================================================
-- 05_pipelines.sql — 流水线定义(含阶段与任务模板)
-- 对应页面: 组件详情-流水线、流水线编辑器
-- 表: pipelines, pipeline_stages, pipeline_task_templates
-- 前缀: d1=pipelines, d2=pipeline_stages, d3=pipeline_task_templates
-- 说明:
--   * type 取值必须是 runner CRD 枚举: Build / Release / Approval
--   * release_config 对应 runner ReleaseSpec(source: Chart|Manifest)
--   * approval_config 对应 Approval 任务配置
-- ============================================================

-- ---------- order-service 构建流水线 ----------
INSERT INTO pipelines (id, component_id, name, kind, description, created_by, version, created_at, updated_at) VALUES
('d1000000-0000-0000-0000-000000000001', 'b2000000-0000-0000-0000-000000000004', '订单服务-构建流水线', 'build', '单测 + 镜像构建 + 推送制品库', 'admin', 3, now() - interval '70 days', now() - interval '6 days')
ON CONFLICT (id) DO NOTHING;

INSERT INTO pipeline_stages (id, pipeline_id, name, sequence, created_at, updated_at) VALUES
('d2000000-0000-0000-0000-000000000001', 'd1000000-0000-0000-0000-000000000001', '测试', 1, now() - interval '70 days', now() - interval '6 days'),
('d2000000-0000-0000-0000-000000000002', 'd1000000-0000-0000-0000-000000000001', '构建', 2, now() - interval '70 days', now() - interval '6 days')
ON CONFLICT (id) DO NOTHING;

-- 注意: pipeline_task_templates 无 created_at/updated_at 列(模型未含时间戳)
INSERT INTO pipeline_task_templates (id, stage_id, name, type, display_order, image, script_path, script_args, command, args, produces, consumes, retry_policy, timeout_seconds) VALUES
('d3000000-0000-0000-0000-000000000001', 'd2000000-0000-0000-0000-000000000001', '单元测试', 'Build', 1,
 'golang:1.22', 'build.sh', '[]'::jsonb, '[]'::jsonb, '[]'::jsonb,
 '["report"]'::jsonb, '[]'::jsonb, '{"maxRetries":1}', 600),
('d3000000-0000-0000-0000-000000000002', 'd2000000-0000-0000-0000-000000000002', '构建镜像', 'Build', 1,
 'golang:1.22', 'build.sh', '[]'::jsonb, '[]'::jsonb, '[]'::jsonb,
 '["image"]'::jsonb, '["report"]'::jsonb, '{"maxRetries":0}', 900)
ON CONFLICT (id) DO NOTHING;

-- ---------- order-service 发布流水线(Chart + 审批 + 金丝雀) ----------
INSERT INTO pipelines (id, component_id, name, kind, description, created_by, version, created_at, updated_at) VALUES
('d1000000-0000-0000-0000-000000000002', 'b2000000-0000-0000-0000-000000000004', '订单服务-发布流水线', 'release', '人工审批后按金丝雀步骤发布 Chart', 'admin', 2, now() - interval '68 days', now() - interval '4 days')
ON CONFLICT (id) DO NOTHING;

INSERT INTO pipeline_stages (id, pipeline_id, name, sequence, created_at, updated_at) VALUES
('d2000000-0000-0000-0000-000000000003', 'd1000000-0000-0000-0000-000000000002', '审批', 1, now() - interval '68 days', now() - interval '4 days'),
('d2000000-0000-0000-0000-000000000004', 'd1000000-0000-0000-0000-000000000002', '部署', 2, now() - interval '68 days', now() - interval '4 days')
ON CONFLICT (id) DO NOTHING;

INSERT INTO pipeline_task_templates (id, stage_id, name, type, display_order, image, script_path, script_args, command, args, produces, consumes, release_config, rollout_config, approval_config, retry_policy, timeout_seconds) VALUES
('d3000000-0000-0000-0000-000000000003', 'd2000000-0000-0000-0000-000000000003', '发布审批', 'Approval', 1,
 '', '', '[]'::jsonb, '[]'::jsonb, '[]'::jsonb, '[]'::jsonb, '[]'::jsonb,
 NULL, NULL,
 '{"approvers":["admin"],"minApprovals":1,"timeoutHours":24}'::jsonb,
 '{"maxRetries":0}', 0),
('d3000000-0000-0000-0000-000000000004', 'd2000000-0000-0000-0000-000000000004', '部署 Chart(金丝雀)', 'Release', 1,
 'alpine/helm:3.14', '', '[]'::jsonb, '[]'::jsonb, '[]'::jsonb, '[]'::jsonb, '[]'::jsonb,
 '{"source":"Chart","chart":{"repoURL":"https://charts.example.com","name":"order-service","version":"1.2.3"},"values":{"replicas":"3"},"targetNamespace":"platform-eng-order-service-prod"}'::jsonb,
 '{"steps":[{"weight":20,"pauseMinutes":5},{"weight":60,"pauseMinutes":5},{"weight":100,"pauseMinutes":0}]}',
 NULL,
 '{"maxRetries":1}', 1800)
ON CONFLICT (id) DO NOTHING;

-- ---------- api-gateway 构建流水线 ----------
INSERT INTO pipelines (id, component_id, name, kind, description, created_by, version, created_at, updated_at) VALUES
('d1000000-0000-0000-0000-000000000003', 'b2000000-0000-0000-0000-000000000001', '网关-构建流水线', 'build', '单测 + 镜像构建', 'admin', 1, now() - interval '60 days', now() - interval '60 days')
ON CONFLICT (id) DO NOTHING;

INSERT INTO pipeline_stages (id, pipeline_id, name, sequence, created_at, updated_at) VALUES
('d2000000-0000-0000-0000-000000000005', 'd1000000-0000-0000-0000-000000000003', '构建', 1, now() - interval '60 days', now() - interval '60 days')
ON CONFLICT (id) DO NOTHING;

INSERT INTO pipeline_task_templates (id, stage_id, name, type, display_order, image, script_path, script_args, command, args, produces, consumes, retry_policy, timeout_seconds) VALUES
('d3000000-0000-0000-0000-000000000005', 'd2000000-0000-0000-0000-000000000005', '构建镜像', 'Build', 1,
 'golang:1.22', 'build.sh', '[]'::jsonb, '[]'::jsonb, '[]'::jsonb,
 '["image"]'::jsonb, '[]'::jsonb, '{"maxRetries":0}', 900)
ON CONFLICT (id) DO NOTHING;

-- ---------- 流水线版本快照(结构化编辑留档) ----------
INSERT INTO pipeline_versions (id, pipeline_id, version, snapshot, created_by, created_at) VALUES
('d4000000-0000-0000-0000-000000000001', 'd1000000-0000-0000-0000-000000000001', 3,
 '{"stages":[{"name":"测试","sequence":1,"tasks":["单元测试"]},{"name":"构建","sequence":2,"tasks":["构建镜像"]}]}',
 'admin', now() - interval '6 days')
ON CONFLICT (id) DO NOTHING;
