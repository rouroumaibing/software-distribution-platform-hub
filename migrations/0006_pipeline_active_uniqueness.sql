-- =====================================================================
-- 0006_pipeline_active_uniqueness.sql — pipelines 的"活行唯一"约束
-- 运行: 手动执行（与 0004/0005 同路径）。语句幂等，AutoMigrate 之后跑亦可。
--   kubectl -n sdp-workflow exec -i deploy/postgres -- \
--     psql -v ON_ERROR_STOP=1 -U sdp -d sdp < migrations/0006_pipeline_active_uniqueness.sql
--
-- 背景（backlog B-15 第 (b) 项）:
--   pipelines 是软删表（`internal/common/base.go` 的 common.Base 带 deleted_at）。
--   而 0001 里写的是 `unique (component_id, name)` —— 不含 deleted_at，于是
--   软删一条流水线后，同名流水线**再也建不回来**：INSERT 撞唯一键 → 500。
--
-- 修法: 换成 partial unique index `where deleted_at is null`。
--   已删流水线的名字可被复用；同一组件下仍禁止两条同名活流水线。
--
-- 说明:
--   * drop constraint 与 create index 都带 if (not) exists，可重复执行；
--   * 运行时表结构由 GORM AutoMigrate 管理（见 internal/db/db.go），模型侧已用
--     uniqueIndex 标签声明同名索引 —— 本文件只解决"AutoMigrate 不会删旧约束"
--     这一缺口，给从 0001 建库的环境补上；
--   * 若目标库里已存在同名活流水线（例如纯 AutoMigrate 建库期间没有唯一约束），
--     建索引会报 duplicate key，需人工先改重名再执行。
-- =====================================================================

alter table pipelines drop constraint if exists pipelines_component_id_name_key;

create unique index if not exists idx_pipelines_component_name_active
    on pipelines(component_id, name) where deleted_at is null;
