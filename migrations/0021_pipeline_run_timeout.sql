-- 0021: pipeline_runs 运行级超时（O-39，F4.3 运行治理）。
-- timeout_seconds>0 时，hub 侧 background ticker 在 startedAt/createdAt + timeout
-- 之后将该 run 置 failed(reason=PipelineRunTimeout)。0 = 不限制（与任务级
-- timeoutSeconds 语义对齐）。幂等。
-- 升级前备份：pg_dump -t pipeline_runs <db> > /tmp/pipeline_runs_pre0021.sql
BEGIN;
ALTER TABLE pipeline_runs ADD COLUMN IF NOT EXISTS timeout_seconds bigint NOT NULL DEFAULT 0;
COMMIT;
