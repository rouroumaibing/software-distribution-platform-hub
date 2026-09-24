-- 0018: agent_ops 全链路补齐（2026-09-23 第十八批，UNIMPLEMENTED-MODULES-PLAN §16.5）。
--
-- 1) agent_op_logs：Runner 执行 agent op 时流式回传的输出分片。分片落库使
--    晚连接 / 断线重连的 SSE 订阅者可完整重放（与 run 侧 TaskRunLog 同一契约）。
--    AutoMigrate 不建表于已有库？——会建（新表），但已有集群由本迁移保证幂等创建。
-- 2) agent_ops.detail varchar(1024) → text：exec 脚本常超 1KiB，派发 payload
--    必须逐字携带命令（AutoMigrate 不改列类型，需手工 ALTER）。

CREATE TABLE IF NOT EXISTS agent_op_logs (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    op_id       uuid NOT NULL,
    seq         integer NOT NULL,
    stream      varchar(16),
    chunk       text,
    created_at  timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_agent_op_logs_op_id ON agent_op_logs (op_id);

ALTER TABLE agent_ops ALTER COLUMN detail TYPE text;
