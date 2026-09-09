-- ============================================================
-- 08_runs_artifacts.sql — 运行记录 + 任务记录 + 制品(演示数据)
-- 对应页面: 运行中心(RunCenter)、组件详情-运行记录/发布/制品库/日志
-- 表: pipeline_runs, task_runs, artifacts
-- 前缀: f2=pipeline_runs, f3=task_runs, f4=artifacts
-- (UUID 只允许 hex 字符, 不用 g 前缀)
-- 说明:
--   * phase 枚举: Pending/Running/WaitingApproval/Succeeded/Failed/Cancelled
--     task_runs.phase: Pending/Running/Succeeded/Failed/Skipped
--   * 真实运行由 runner 回写, 这里只做前端演示种子。
--   * cr_name/cr_namespace 仅在集群内存在对应 CR 时有意义,
--     演示数据指向不存在的 CR 属预期, 不影响列表展示。
-- ============================================================

-- ---------- order-service 构建流水线的运行历史 ----------
INSERT INTO pipeline_runs (id, pipeline_id, cluster_id, cr_name, cr_namespace, commit_sha, params, pipeline_version, phase, triggered_by, start_time, completion_time, message, created_at, updated_at) VALUES
-- 成功
('f2000000-0000-0000-0000-000000000001', 'd1000000-0000-0000-0000-000000000001', 'c1000000-0000-0000-0000-000000000001',
 'pipelinerun-order-0001', 'sdp-system', '3f1c9ab12de4', '{}'::jsonb, 3, 'Succeeded', 'admin',
 now() - interval '6 days' + interval '10 hours', now() - interval '6 days' + interval '10 hours' + interval '8 minutes', '',
 now() - interval '6 days' + interval '10 hours', now() - interval '6 days' + interval '10 hours' + interval '8 minutes'),
-- 失败(单测挂)
('f2000000-0000-0000-0000-000000000002', 'd1000000-0000-0000-0000-000000000001', 'c1000000-0000-0000-0000-000000000001',
 'pipelinerun-order-0002', 'sdp-system', 'a7b2ee09f3c1', '{}'::jsonb, 3, 'Failed', 'zhang.san',
 now() - interval '4 days' + interval '14 hours', now() - interval '4 days' + interval '14 hours' + interval '3 minutes', 'unit test failed: TestCreateOrder',
 now() - interval '4 days' + interval '14 hours', now() - interval '4 days' + interval '14 hours' + interval '3 minutes'),
-- 运行中
('f2000000-0000-0000-0000-000000000003', 'd1000000-0000-0000-0000-000000000001', 'c1000000-0000-0000-0000-000000000001',
 'pipelinerun-order-0003', 'sdp-system', '9d0c4fa77e21', '{}'::jsonb, 3, 'Running', 'admin',
 now() - interval '25 minutes', NULL, '',
 now() - interval '25 minutes', now() - interval '25 minutes')
ON CONFLICT (id) DO NOTHING;

-- ---------- order-service 发布流水线(等待审批, 发布页演示) ----------
INSERT INTO pipeline_runs (id, pipeline_id, cluster_id, cr_name, cr_namespace, commit_sha, params, pipeline_version, phase, triggered_by, start_time, completion_time, message, created_at, updated_at) VALUES
('f2000000-0000-0000-0000-000000000004', 'd1000000-0000-0000-0000-000000000002', 'c1000000-0000-0000-0000-000000000003',
 'pipelinerun-order-rel-0001', 'sdp-system', '3f1c9ab12de4', '{"environment":"prod"}'::jsonb, 2, 'WaitingApproval', 'admin',
 now() - interval '2 hours', NULL, '等待发布审批(approvers: admin)',
 now() - interval '2 hours', now() - interval '2 hours')
ON CONFLICT (id) DO NOTHING;

-- ---------- api-gateway 构建流水线(成功) ----------
INSERT INTO pipeline_runs (id, pipeline_id, cluster_id, cr_name, cr_namespace, commit_sha, params, pipeline_version, phase, triggered_by, start_time, completion_time, message, created_at, updated_at) VALUES
('f2000000-0000-0000-0000-000000000005', 'd1000000-0000-0000-0000-000000000003', 'c1000000-0000-0000-0000-000000000001',
 'pipelinerun-gateway-0001', 'sdp-system', 'c88d10b6aa05', '{}'::jsonb, 1, 'Succeeded', 'li.si',
 now() - interval '3 days' + interval '9 hours', now() - interval '3 days' + interval '9 hours' + interval '6 minutes', '',
 now() - interval '3 days' + interval '9 hours', now() - interval '3 days' + interval '9 hours' + interval '6 minutes')
ON CONFLICT (id) DO NOTHING;

-- ---------- 成功构建 run #1 的任务明细(运行详情 + 日志页演示) ----------
INSERT INTO task_runs (id, pipeline_run_id, task_template_id, cr_name, task_name, stage_name, type, phase, retry_count, exit_code, start_time, completion_time, logs_ref, message, created_at, updated_at) VALUES
('f3000000-0000-0000-0000-000000000001', 'f2000000-0000-0000-0000-000000000001', 'd3000000-0000-0000-0000-000000000001',
 'pipelinerun-order-0001-unit-test', '单元测试', '测试', 'Build', 'Succeeded', 0, 0,
 now() - interval '6 days' + interval '10 hours', now() - interval '6 days' + interval '10 hours' + interval '3 minutes',
 's3://sdp-logs/order-0001/unit-test.log', '',
 now() - interval '6 days' + interval '10 hours', now() - interval '6 days' + interval '10 hours' + interval '3 minutes'),
('f3000000-0000-0000-0000-000000000002', 'f2000000-0000-0000-0000-000000000001', 'd3000000-0000-0000-0000-000000000002',
 'pipelinerun-order-0001-build-image', '构建镜像', '构建', 'Build', 'Succeeded', 0, 0,
 now() - interval '6 days' + interval '10 hours' + interval '3 minutes', now() - interval '6 days' + interval '10 hours' + interval '8 minutes',
 's3://sdp-logs/order-0001/build-image.log', '',
 now() - interval '6 days' + interval '10 hours' + interval '3 minutes', now() - interval '6 days' + interval '10 hours' + interval '8 minutes'),
-- 失败 run 的任务明细(日志页演示失败输出)
('f3000000-0000-0000-0000-000000000003', 'f2000000-0000-0000-0000-000000000002', 'd3000000-0000-0000-0000-000000000001',
 'pipelinerun-order-0002-unit-test', '单元测试', '测试', 'Build', 'Failed', 1, 1,
 now() - interval '4 days' + interval '14 hours', now() - interval '4 days' + interval '14 hours' + interval '3 minutes',
 's3://sdp-logs/order-0002/unit-test.log', 'exit status 1: TestCreateOrder assertion failed',
 now() - interval '4 days' + interval '14 hours', now() - interval '4 days' + interval '14 hours' + interval '3 minutes'),
('f3000000-0000-0000-0000-000000000004', 'f2000000-0000-0000-0000-000000000002', 'd3000000-0000-0000-0000-000000000002',
 'pipelinerun-order-0002-build-image', '构建镜像', '构建', 'Build', 'Skipped', 0, NULL,
 NULL, NULL, '', '上游阶段失败, 跳过',
 now() - interval '4 days' + interval '14 hours' + interval '3 minutes', now() - interval '4 days' + interval '14 hours' + interval '3 minutes')
ON CONFLICT (id) DO NOTHING;

-- ---------- 制品库(挂在成功的 run 上) ----------
INSERT INTO artifacts (id, component_id, pipeline_run_id, task_run_id, version, artifact_type, storage_key, size_bytes, checksum, commit_sha, created_at) VALUES
('f4000000-0000-0000-0000-000000000001', 'b2000000-0000-0000-0000-000000000004', 'f2000000-0000-0000-0000-000000000001', 'f3000000-0000-0000-0000-000000000002',
 'v1.2.3', 'image', 'registry.local:5000/platform-eng/order-service:v1.2.3', 48211000, 'sha256:1f9c4e...a2', '3f1c9ab12de4',
 now() - interval '6 days' + interval '10 hours' + interval '8 minutes'),
('f4000000-0000-0000-0000-000000000002', 'b2000000-0000-0000-0000-000000000001', 'f2000000-0000-0000-0000-000000000005', NULL,
 'v0.9.1', 'image', 'registry.local:5000/platform-eng/api-gateway:v0.9.1', 30120000, 'sha256:8bc7d0...5e', 'c88d10b6aa05',
 now() - interval '3 days' + interval '9 hours' + interval '6 minutes')
ON CONFLICT (id) DO NOTHING;
