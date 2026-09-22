-- =====================================================================
-- 0013_pipeline_versions.sql
--   流水线定义版本历史（C-09）：唯一约束 + 扫描索引
--   运行: 手动执行（与 0004~0012 同路径）。语句幂等，可重复跑。
--     kubectl -n sdp-workflow exec -i deploy/postgres -- \
--       psql -v ON_ERROR_STOP=1 -U sdp -d sdp < migrations/0013_pipeline_versions.sql
--
-- 背景:
--   `pipeline_versions` 表本身早在 `internal/db/db.go` 的 AutoMigrate 清单里
--   （模型 `PipelineVersion` 一直存在），但**从来没有人写过一行** ——
--   `VersionRepository.Create` 没有任何调用方（plans/UNIMPLEMENTED-MODULES-PLAN.md
--   称它为 "PublishVersion 占位"）。C-09 让结构改动真正留档，于是这张表第一次
--   有数据，也就第一次需要它自己的约束。
--
-- 说明:
--   * `unique (pipeline_id, version)` 是本功能的**核心不变量**：版本号是面向用户
--     的寻址键（对比 / 回滚都按号码取），一旦允许重复，"回滚到 v3"就成了二义性
--     问题。索引同时由 GORM 的 `uniqueIndex` 标签声明（AutoMigrate 会建），本文件
--     再显式建一次，让「纯 AutoMigrate 建库」与「手跑 SQL 建库」两条路径收敛。
--   * 该唯一性**不含 `deleted_at`**（本表无软删列）：版本历史是 append-only 的，
--     行永不删除，也就没有"已删行占住版本号"的问题 —— 与 pipelines /
--     pipeline_stages 的 partial unique index 场景不同，那里才需要 `where
--     deleted_at is null`。
--   * 按 (pipeline_id, version desc) 取"最新一版"与列表页都是热路径，
--     故再补一条 (pipeline_id, version) 之外的 created_at 索引供审计排序。
--
-- ⚠️ 若第 1 步报 duplicate key:
--   说明库里已有重复 (pipeline_id, version) 行（历史遗留 / 手工写入）。**先查再改**:
--     select pipeline_id, version, count(*) from pipeline_versions
--      group by 1, 2 having count(*) > 1;
--   人工判重后再跑本文件。C-09 上线前的库不会有数据，正常情况该查询返回 0 行。
-- =====================================================================

-- 1) (pipeline_id, version) 唯一：版本号是寻址键，不允许二义
create unique index if not exists idx_pipeline_versions_pipeline_version
    on pipeline_versions (pipeline_id, version);

-- 2) 审计 / 排序辅助索引
create index if not exists idx_pipeline_versions_pipeline_created
    on pipeline_versions (pipeline_id, created_at desc);
