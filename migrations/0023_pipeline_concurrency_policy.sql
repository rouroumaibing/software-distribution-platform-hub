-- 0023: O-41 same-pipeline concurrency / queueing policy (F4.2).
-- pipelines.concurrency_policy: 'allow' (default, legacy behavior) | 'queue'
-- (new runs park as PendingQueue once the active-run limit N is reached) |
-- 'forbid' (reject new triggers with 409 while an active run exists).
-- The limit N itself is runtime config (HUB_RUN_CONCURRENCY_LIMIT), not schema.
ALTER TABLE pipelines
    ADD COLUMN IF NOT EXISTS concurrency_policy varchar(16) NOT NULL DEFAULT 'allow';
