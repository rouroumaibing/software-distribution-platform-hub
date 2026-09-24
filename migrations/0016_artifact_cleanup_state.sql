-- 0016_artifact_cleanup_state.sql
-- B-16 收口：让 artifacts.expires_at 真正生效（保留期 GC），并为"对象删失败"
-- 提供一个**可重试的持久状态**（cleanup_state）。
--
-- 为什么需要这一列：删对象是不可逆的外部动作、可能失败。旧实现只把失败写成一条
-- 日志（重启即丢），而对账作业又是"仅报告 + 默认关闭"。cleanup_state 让失败在**库里**
-- 留下痕迹，于是 hub 可以**自己重试**，而不是等人读报告。
--
-- 与 AutoMigrate 的关系：模型已声明该列，AutoMigrate 会补列；本迁移的增量是
-- **CHECK 约束 + 两个索引**（AutoMigrate 不建 CHECK，索引形状也需与查询谓词对齐）。
--
-- 幂等：可重复执行。
--
-- 手工执行（AutoMigrate 不会跑本文件；本机无 Postgres，故未经真实执行验证）：
--   psql "$DB" -f migrations/0016_artifact_cleanup_state.sql
--
-- 回滚（如确需）：
--   ALTER TABLE artifacts DROP CONSTRAINT IF EXISTS artifacts_cleanup_state_check;
--   DROP INDEX IF EXISTS idx_artifacts_expires_at;
--   DROP INDEX IF EXISTS idx_artifacts_component_cleanup;
--   ALTER TABLE artifacts DROP COLUMN IF EXISTS cleanup_state;
BEGIN;

-- 1) 清理状态列。默认 'active'：既有行都是"正常"，语义正确。
ALTER TABLE artifacts
    ADD COLUMN IF NOT EXISTS cleanup_state varchar(32) NOT NULL DEFAULT 'active';

-- 2) 取值域用 DDL CHECK 钉住（与 ACCOUNT-PERMISSION-MODEL §5.3 的"语义落库"一致）：
--      active           = 正常（行与对象都在）
--      pending_deletion = 已过期、对象尚未删成功；行**刻意保留**以便重试
ALTER TABLE artifacts DROP CONSTRAINT IF EXISTS artifacts_cleanup_state_check;
ALTER TABLE artifacts
    ADD CONSTRAINT artifacts_cleanup_state_check
    CHECK (cleanup_state IN ('active', 'pending_deletion'));

-- 3) GC 的查询谓词是 `expires_at IS NOT NULL AND expires_at < $1`，按 expires_at 升序。
--    部分索引只覆盖"有保留期的行"：多数制品 expires_at 为 NULL，不该进索引。
CREATE INDEX IF NOT EXISTS idx_artifacts_expires_at
    ON artifacts (expires_at)
    WHERE expires_at IS NOT NULL;

-- 4) 列表查询：WHERE component_id = $1 AND cleanup_state <> 'pending_deletion'，按 created_at 倒序。
--    前两列即 (component_id, cleanup_state)，让过滤走索引而不是全表扫。
CREATE INDEX IF NOT EXISTS idx_artifacts_component_cleanup
    ON artifacts (component_id, cleanup_state);

COMMIT;
