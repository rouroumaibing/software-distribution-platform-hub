-- =====================================================================
-- 0008_config_history_env_key.sql — 配置审计去 FK + 环境 key 快照
-- 运行: 手动执行（与 0004~0007 同路径）。语句幂等，可重复跑。
--   kubectl -n sdp-workflow exec -i deploy/postgres -- \
--     psql -v ON_ERROR_STOP=1 -U sdp -d sdp < migrations/0008_config_history_env_key.sql
--
-- 背景（backlog B-14 / DELETE-CONTRACT §6.6-2 决策 2）:
--   component_config_history.environment_id 是 `uuid references environments(id)`
--   且**没有 on delete**（0002_component_management.sql:93）→ NO ACTION。
--   而环境删除是**硬删**（environments 嵌 BaseNoSoftDelete，environment/service
--   的 Delete 直删）→ 只要审计表里已有该环境的行，删环境就撞 FK 报错塌成 500；
--   一行都没有就又静默成功。同一个操作两种结局，取决于历史 —— 典型的
--   "取决于历史的随机失败"。
--
--   "SET NULL"能消掉 500，但会破坏溯源：删环境后历史行的 environment_id 变
--   NULL，与"全局默认值"行（environment_id 本来就为 NULL）无法区分，
--   "某人在 alpha 改了 REGISTRY"退化成"某人改了 REGISTRY（环境未知）"。
--   审计表的唯一目的就是溯源，故取"去 FK + 加快照列"：
--     * environment_id 保留为**尽力引用**（仅去约束，列不删）；
--     * environment_key 冗余写入当时的环境 key，环境删掉后历史仍可读。
--
-- 说明:
--   * environment_key 列也会由 AutoMigrate 补（`component/models/config.go` 的
--     ComponentConfigHistory 已加字段）；本文件用 if not exists 保证
--     "从 0001 建库"与"纯 AutoMigrate 建库"两条路径收敛到同一结构；
--   * 约束默认名由 0002 的 `references` 内联生成 →
--     component_config_history_environment_id_fkey。带 if exists，可重复跑；
--   * **不做猜测式回填**：已删环境的历史行 environment_key 保持 NULL
--     （写入当时还没有这一列）。
-- =====================================================================

alter table component_config_history
    drop constraint if exists component_config_history_environment_id_fkey;

alter table component_config_history
    add column if not exists environment_key varchar(64);

-- 自检: 约束应已不存在
do $$
declare n int;
begin
  select count(*) into n from pg_constraint
   where conname = 'component_config_history_environment_id_fkey';
  if n > 0 then
    raise warning '0008: component_config_history_environment_id_fkey 仍存在，请检查';
  end if;
end $$;
