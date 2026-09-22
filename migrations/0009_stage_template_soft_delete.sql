-- =====================================================================
-- 0009_stage_template_soft_delete.sql — stage/template 软删 + 活行唯一
-- 运行: 手动执行（与 0004~0008 同路径）。语句幂等，可重复跑。
--   kubectl -n sdp-workflow exec -i deploy/postgres -- \
--     psql -v ON_ERROR_STOP=1 -U sdp -d sdp < migrations/0009_stage_template_soft_delete.sql
--
-- 背景（backlog B-15 剩余项 / DELETE-CONTRACT §6.6-3 决策 3）:
--   pipeline_stages / pipeline_task_templates 原本是硬删表（无 deleted_at）。
--   而 pipeline 是软删（run 历史必须比定义活得久，见 common/base.go），
--   "父软删、子硬删"会产出**半状态**：恢复一条流水线只得到空壳。
--   故两表补 deleted_at，改随父软删（模型已从 BaseNoSoftDelete 换成 Base）。
--
--   补列之后旧唯一约束必须同步改造，否则引入新的 500：
--     pipeline_stages          `unique (pipeline_id, sequence)`
--     pipeline_task_templates  `unique (stage_id, name)`
--   二者都不含 deleted_at → 软删一个 stage/模板后，再建同 sequence / 同名的行
--   会撞旧约束直接失败。改成 partial unique index（where deleted_at is null），
--   与 pipelines 的 idx_pipelines_component_name_active（0006）完全同手法。
--
-- 说明:
--   * deleted_at 列由 AutoMigrate 补（模型已换 Base）；本文件也显式
--     if not exists 加一次，保证两条建库路径收敛；
--   * 纯 AutoMigrate 建库的库里，两条旧 unique 约束**从未被创建过**（旧模型
--     没有任何唯一性 tag）—— 那里的缺陷方向是反的：允许同一组件下出现重复的
--     活 stage/模板。partial unique index 两个方向一起修正；
--   * 若目标库里已存在重复的活行，建索引会报 duplicate key，需先人工去重。
-- =====================================================================

-- 1) 补软删列（AutoMigrate 也会加，这里保证幂等一致）
alter table pipeline_stages         add column if not exists deleted_at timestamptz;
alter table pipeline_task_templates add column if not exists deleted_at timestamptz;

create index if not exists idx_pipeline_stages_deleted_at
    on pipeline_stages(deleted_at);
create index if not exists idx_pipeline_task_templates_deleted_at
    on pipeline_task_templates(deleted_at);

-- 2) 旧唯一约束 → 活行 partial unique index
alter table pipeline_stages
    drop constraint if exists pipeline_stages_pipeline_id_sequence_key;
create unique index if not exists idx_stages_pipeline_seq_active
    on pipeline_stages(pipeline_id, sequence) where deleted_at is null;

alter table pipeline_task_templates
    drop constraint if exists pipeline_task_templates_stage_id_name_key;
create unique index if not exists idx_task_templates_stage_name_active
    on pipeline_task_templates(stage_id, name) where deleted_at is null;
