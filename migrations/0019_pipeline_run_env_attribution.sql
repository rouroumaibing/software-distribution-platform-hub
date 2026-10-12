-- 0019: pipeline_runs denormalized attribution (ui-12 §5.1 环境覆盖档 /
-- §6.3 配额 org 维度). org_id arrives via pipeline→component resolution;
-- environment_id via the (component, target, namespace) unique match at
-- trigger time. Nullable on purpose: attribution is best-effort (fail-open)
-- and legacy rows have no binding. Idempotent via IF NOT EXISTS.
ALTER TABLE pipeline_runs ADD COLUMN IF NOT EXISTS org_id uuid;
ALTER TABLE pipeline_runs ADD COLUMN IF NOT EXISTS environment_id uuid;
CREATE INDEX IF NOT EXISTS idx_pipeline_runs_org ON pipeline_runs(org_id);
CREATE INDEX IF NOT EXISTS idx_pipeline_runs_environment ON pipeline_runs(environment_id);
