-- =====================================================================
-- 0010_stage_execution_mode.sql — pipeline_stages 补 execution_mode
-- 运行: 手动执行（与 0004~0009 同路径）。语句幂等，可重复跑。
--   kubectl -n sdp-workflow exec -i deploy/postgres -- \
--     psql -v ON_ERROR_STOP=1 -U sdp -d sdp < migrations/0010_stage_execution_mode.sql
--
-- 背景:
--   `stages[].executionMode`（parallel | serial，stage 级）**契约与 DDL 早已拍板**
--   ——见 hub/API-REFERENCE.md 的字段表、hub/DATA-MODEL.md §6.4-①（那里连 ALTER
--   语句都写好了），console 原型 CONSOLE-UI-原型.html 的阶段头开关也早已实现。
--   但 hub 的 PipelineStage 结构体一直没有该字段（DATA-MODEL 自己标注
--   「⚠️ 当前未实现」），于是 console 的阶段「并行/串行」开关无处落库。
--   本迁移 + 模型字段补齐「存得下、读得回」这一半。
--
-- 说明:
--   * 列本身由 AutoMigrate 补（模型已声明 size:16 / not null / default:parallel）；
--     本文件也显式 add column if not exists 加一次，保证"纯 AutoMigrate 建库"与
--     "手跑 SQL 建库"两条路径收敛。
--   * AutoMigrate 加 NOT NULL 列依赖 DEFAULT 生效，老行若在 DEFAULT 生效前写入会
--     留空值，故显式回填一次。
--   * 枚举约束写进 DDL（CHECK），不只在应用层校验 —— 应用层校验挡不住手工 SQL。
--   * 取值一律**小写**（parallel/serial），以面向客户端的 API 契约为准；
--     DATA-MODEL §6.4-① 的草案 ALTER 用的是 'Parallel'（首字母大写），该文档已
--     同步改为小写，避免两处权威文档互相矛盾。
--
-- ⚠️ 本迁移只加列，**不改调度行为**:
--   Serial 的调度实现（同阶段按序派生 DependsOn 链）属 runner 侧 backlog C-06，
--   仍未做（见 runner/STORY-runner-implementation.md 与 docs/hub/STORY-BACKLOG.md）。
--   当前该字段只做 API ↔ DB 往返；serial 阶段的子任务在 runner 侧仍会并发启动。
-- =====================================================================

-- 1) 补列（AutoMigrate 也会加，这里保证幂等一致）
alter table pipeline_stages
    add column if not exists execution_mode varchar(16) not null default 'parallel';

-- 2) 回填（DEFAULT 生效前写入的老行）
update pipeline_stages set execution_mode = 'parallel'
 where execution_mode is null or execution_mode = '';

-- 3) 枚举约束写进 DDL（幂等：先 drop 再 add）
alter table pipeline_stages
    drop constraint if exists pipeline_stages_execution_mode_check;
alter table pipeline_stages
    add constraint pipeline_stages_execution_mode_check
    check (execution_mode in ('parallel', 'serial'));
