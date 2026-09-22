-- =====================================================================
-- 0007_rename_clusters_to_targets.sql — 「集群注册表」改名「接入目标注册表」
-- 运行: 手动执行（与 0004/0005/0006 同路径），语句幂等，可重复跑。
--   kubectl -n sdp-workflow exec -i deploy/postgres -- \
--     psql -v ON_ERROR_STOP=1 -U sdp -d sdp < migrations/0007_rename_clusters_to_targets.sql
--
-- 背景（2026-09-21 裁定）: 「接入目标」语义上不预设类型 —— 一个 Target 可以
--   是一个 K8s 集群（agent / kubeconfig 通道），也可以是一台直连主机
--   （ssh 通道）。故 clusters 表改名 targets、引用列 cluster_id 改名
--   target_id；console 侧菜单同步改名「接入管理」，路由 /admin/targets。
--
-- 为什么必须显式迁移: hub 建表靠启动时 GORM AutoMigrate，而 AutoMigrate
--   只加列不删列、更不会改名 —— 若不先跑本文件，旧列 cluster_id 会与新列
--   target_id 并存，且 cluster_id 的 NOT NULL 会让新 INSERT 直接失败。
--   （dispatch_jobs 表尤其如此：它只由 AutoMigrate 创建，不在 0001 里。）
-- =====================================================================

-- 1) 表改名
do $$
begin
  if exists (select 1 from information_schema.tables
             where table_schema = 'public' and table_name = 'clusters') then
    alter table clusters rename to targets;
  end if;
end $$;

-- 2) 随之改索引/约束名
do $$
begin
  if exists (select 1 from pg_indexes where indexname = 'clusters_pkey') then
    alter index clusters_pkey rename to targets_pkey;
  end if;
  if exists (select 1 from pg_indexes where indexname = 'clusters_name_key') then
    alter index clusters_name_key rename to targets_name_key;
  end if;
  if exists (select 1 from pg_indexes where indexname = 'idx_environments_cluster') then
    alter index idx_environments_cluster rename to idx_environments_target;
  end if;
end $$;

-- 3) 引用列改名（environments / pipeline_runs / dispatch_jobs）
do $$
begin
  if exists (select 1 from information_schema.columns
             where table_schema = 'public' and table_name = 'environments' and column_name = 'cluster_id') then
    alter table environments rename column cluster_id to target_id;
  end if;
end $$;

do $$
begin
  if exists (select 1 from information_schema.columns
             where table_schema = 'public' and table_name = 'pipeline_runs' and column_name = 'cluster_id') then
    alter table pipeline_runs rename column cluster_id to target_id;
  end if;
end $$;

do $$
begin
  if exists (select 1 from information_schema.columns
             where table_schema = 'public' and table_name = 'dispatch_jobs' and column_name = 'cluster_id') then
    alter table dispatch_jobs rename column cluster_id to target_id;
  end if;
end $$;

-- 4) 自检：迁移后不应再有 cluster_id 列
do $$
declare n int;
begin
  select count(*) into n from information_schema.columns
   where table_schema = 'public' and column_name = 'cluster_id';
  if n > 0 then
    raise warning '0007: 仍有 % 个 cluster_id 列未改名，请检查', n;
  end if;
end $$;
