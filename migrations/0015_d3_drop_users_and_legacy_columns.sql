-- ============================================================
-- 0015_d3_drop_users_and_legacy_columns.sql
-- D3「hub 不存用户表」的**不可逆**落地。
--   规范: hub/ACCOUNT-PERMISSION-MODEL.md §2.2 / §5.3 / §12 D3
--   执行清单: plans/ACCOUNT-PERMISSION-DECISIONS.md §3.5 第 5 条
--
-- ⚠️ 这是本仓第一份「删列 + 删表」的迁移。AutoMigrate 做不了这件事（它只加不删），
--    所以必须手工执行；删表不可逆。执行前先备份：
--        pg_dump -U sdp -d sdp -Fc -f sdp-before-0015.dump
--
-- 为什么删得掉：hub 对身份改为**无状态** —— 主体一律取 token 的 `sub`（§5.3），
-- 不再有本地用户行，因此 `users` 表、指向它的 `owner_user`，以及 V1 遗留的
-- `user_id`/`role_id` 都没有读取点了。
--
-- 执行顺序（重要，别调换 —— 2026-09-23 真库实测修正，早先写的顺序是反的）：
--   0) 备份：pg_dump -U sdp -d sdp -Fc -f sdp-before-0015.dump
--   1) **先停掉旧 hub**，再跑本文件。必须在**新版 hub 启动之前**（原因见下）。
--   2) 再部署新版 hub（AutoMigrate）。此时入向 FK 已摘、作者列已转成 varchar，
--      AutoMigrate 能正常跑完（并补齐 owner_sub 与其余新表）。
--   3) 其余运维集合 0006..0014 / 0016 可在任意时点跑，幂等。
--
--   为什么不能"先上新版、再跑本文件"（实测报错，别再照旧注释做）：
--     老库 + 新版 hub 启动 → AutoMigrate 要把 component_configs.created_by 从 uuid
--     改成 varchar(128)，而指向 users(id) 的 FK 还在，Postgres 直接拒绝：
--       ERROR: foreign key constraint "component_configs_created_by_fkey"
--              cannot be implemented (SQLSTATE 42804)
--       F hub: failed to open db
--     **AutoMigrate 自己也撞同一堵墙**，所以"先上新版"这一步根本走不到本文件。
--   本文件自带 ADD COLUMN IF NOT EXISTS owner_sub，不依赖 AutoMigrate 先跑。
--
--   回填为何必须在本文件内、且先于删列：`users.keycloak_id` 是唯一能把旧
--   `users.id` 映射回 `sub` 的东西，`users` 一删映射就永久丢失。
--
-- 幂等性（2026-09-23 在真实 Postgres 16 上实测修正）：
--   本文件设计为**两条路径都安全**：
--     Path A 全新安装 —— hub 的 AutoMigrate 建好库后跑本文件（全为空操作）；
--     Path B 老库升级 —— `0001` 时代的库升到新版 hub 后跑本文件（执行真实回填）。
--   验证脚本见 `scripts/migration-check.sh`。
--
--   ⚠️ 本行注释此前写作「全部语句带 IF EXISTS / to_regclass 判断」，**当时是假的**：
--      ① 阶段 2 改列类型前没有摘掉指向 `users(id)` 的 FK，Postgres 会因「类型不兼容」
--         拒绝 ALTER（连带 `DROP TABLE users` 也必然失败 —— 还有 FK 依赖它）；
--      ② 阶段 4 无条件引用 `owner_user`，在 Path A（该列已不存在）上直接报错。
--      两个 bug 都只在真实库上暴露，DB-free 的门禁（build/vet/test/DryRun）全都看不到。
--      现已修：阶段 0 动态摘除全部入向 FK，阶段 4 对 `owner_user` 加存在性判断。
--
-- 本批纳入的「规范没列的同类站点」（与 §3.5 第 3 条同性质）：
--   除 D3 清单列出的 4 处作者列外，DB 上还有第 6 条指向 `users(id)` 的 FK 挂在
--   `pipeline_versions.created_by` 上。该列在**模型里早已是 `string`**，只有老库
--   仍留着 uuid + FK —— 属于「两条建库路径必须收敛」的同一问题，故一并转换。
-- ============================================================

BEGIN;

-- ------------------------------------------------------------
-- 阶段 0：摘掉所有**入向**指向 `users` 的外键（必须先于改列类型）
--   为什么必须动态查而不是写死 6 条：写死就会随模型演进而过期，而这里漏一条的
--   后果是「改列类型报一个与 FK 无关的错误」——排查成本极高。动态枚举保证
--   「表上实际有几条就摘几条」，且把每条都打进 NOTICE 便于审计。
--   表不存在（Path A）时整段跳过。
-- ------------------------------------------------------------
DO $$
DECLARE
  r      record;
  n_fk   int := 0;
BEGIN
  IF to_regclass('public.users') IS NULL THEN
    RAISE NOTICE '0015 phase0: no `users` table (fresh install) — no inbound FK to drop';
    RETURN;
  END IF;

  FOR r IN
    SELECT conrelid::regclass::text AS tbl, conname
      FROM pg_constraint
     WHERE contype = 'f'
       AND confrelid = 'users'::regclass
     ORDER BY 1, 2
  LOOP
    EXECUTE format('ALTER TABLE %s DROP CONSTRAINT IF EXISTS %I', r.tbl, r.conname);
    RAISE NOTICE '0015 phase0: dropped FK %.% (was referencing users)', r.tbl, r.conname;
    n_fk := n_fk + 1;
  END LOOP;

  RAISE NOTICE '0015 phase0: % inbound FK(s) referencing users dropped', n_fk;
END $$;

-- ------------------------------------------------------------
-- 阶段 1：确保新列存在（AutoMigrate 通常已建；补一次以防手工先跑）
-- ------------------------------------------------------------
ALTER TABLE components ADD COLUMN IF NOT EXISTS owner_sub varchar(128);

-- ------------------------------------------------------------
-- 阶段 2：把作者类列从 uuid 转成 text
--   必须先转类型、再回填 —— 否则 `created_by` 还是 uuid 列，写不进 `sub` 字符串。
--   转类型本身无损（uuid::text 是同一批字节的字符串形式，旧值仍然原样保留，
--   阶段 3 会把它映射成 sub，阶段 4 把映射不到的显式清掉）。
--   重复执行安全：目标已是 varchar 时是空操作。
--   `pipeline_versions.created_by` 见文件头「同类站点」说明。
-- ------------------------------------------------------------
ALTER TABLE component_configs          ALTER COLUMN created_by TYPE varchar(128) USING created_by::text;
ALTER TABLE component_configs          ALTER COLUMN updated_by TYPE varchar(128) USING updated_by::text;
ALTER TABLE component_config_history   ALTER COLUMN changed_by TYPE varchar(128) USING changed_by::text;
ALTER TABLE component_role_bindings    ALTER COLUMN granted_by TYPE varchar(128) USING granted_by::text;
ALTER TABLE pipeline_versions          ALTER COLUMN created_by TYPE varchar(128) USING created_by::text;

-- ------------------------------------------------------------
-- 阶段 3：回填 —— 旧 `users.id` → `users.keycloak_id`（= token `sub`）
--   `users` 表可能压根不存在（Path A：AutoMigrate 已不再建它），
--   所以整段包在 to_regclass 判断里，缺表就跳过而不是报错。
-- ------------------------------------------------------------
DO $$
DECLARE
  n_owner     int := 0;
  n_created   int := 0;
  n_updated   int := 0;
  n_changed   int := 0;
  n_granted   int := 0;
  n_verauth   int := 0;
BEGIN
  IF to_regclass('public.users') IS NULL THEN
    RAISE NOTICE '0015 phase3: no `users` table (fresh install) — nothing to backfill';
    RETURN;
  END IF;

  UPDATE components c
     SET owner_sub = u.keycloak_id
    FROM users u
   WHERE c.owner_user = u.id
     AND c.owner_sub IS NULL;
  GET DIAGNOSTICS n_owner = ROW_COUNT;

  UPDATE component_configs c
     SET created_by = u.keycloak_id
    FROM users u
   WHERE c.created_by = u.id::text;
  GET DIAGNOSTICS n_created = ROW_COUNT;

  UPDATE component_configs c
     SET updated_by = u.keycloak_id
    FROM users u
   WHERE c.updated_by = u.id::text;
  GET DIAGNOSTICS n_updated = ROW_COUNT;

  UPDATE component_config_history h
     SET changed_by = u.keycloak_id
    FROM users u
   WHERE h.changed_by = u.id::text;
  GET DIAGNOSTICS n_changed = ROW_COUNT;

  UPDATE component_role_bindings b
     SET granted_by = u.keycloak_id
    FROM users u
   WHERE b.granted_by = u.id::text;
  GET DIAGNOSTICS n_granted = ROW_COUNT;

  UPDATE pipeline_versions v
     SET created_by = u.keycloak_id
    FROM users u
   WHERE v.created_by = u.id::text;
  GET DIAGNOSTICS n_verauth = ROW_COUNT;

  RAISE NOTICE '0015 phase3 backfill: components.owner_sub=%, configs.created_by=%, configs.updated_by=%, config_history.changed_by=%, bindings.granted_by=%, pipeline_versions.created_by=%',
    n_owner, n_created, n_updated, n_changed, n_granted, n_verauth;
END $$;

-- ------------------------------------------------------------
-- 阶段 4：清掉映射不到的残留（决策文档 §3.5 / §0.2 明确要求「显式置空 + 告警」）
--   此刻作者类列里可能还剩着旧 uuid 的**字面量**：它看起来像主体，却永远解析
--   不到任何人。留着比清掉更危险 —— 它会被后面的读取点当成一个真实 `sub` 用。
--   `owner_user` 侧的分支只在**该列仍存在**时执行（Path A 上它已被模型移除，
--   无条件引用会让全新安装直接崩 —— 这正是本文件此前的第 2 个 bug）。
--   注：owner 丢失**不会**留下授权空洞 —— 创建组件时 P3b 已经把 owner 绑定成
--   component-admin（component_role_bindings 里那条仍在），owner override 只是
--   在绑定被删掉后的兜底。
-- ------------------------------------------------------------
DO $$
DECLARE
  uuid_literal     text := '^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$';
  n_owner_unmapped int := 0;
  n_stale_authors  int := 0;
BEGIN
  IF EXISTS (SELECT 1 FROM information_schema.columns
              WHERE table_schema = 'public' AND table_name = 'components'
                AND column_name = 'owner_user') THEN
    EXECUTE 'SELECT count(*) FROM components WHERE owner_user IS NOT NULL AND owner_sub IS NULL'
       INTO n_owner_unmapped;

    IF n_owner_unmapped > 0 THEN
      RAISE WARNING '0015: % component(s) had owner_user pointing at no users.keycloak_id — owner_sub stays NULL (owner override lost; the P3b component-admin binding is unaffected). Re-assign the owner in the console.', n_owner_unmapped;
    END IF;
  END IF;

  SELECT (SELECT count(*) FROM component_configs        WHERE created_by ~ uuid_literal)
       + (SELECT count(*) FROM component_configs        WHERE updated_by ~ uuid_literal)
       + (SELECT count(*) FROM component_config_history WHERE changed_by ~ uuid_literal)
       + (SELECT count(*) FROM component_role_bindings  WHERE granted_by ~ uuid_literal)
       + (SELECT count(*) FROM pipeline_versions        WHERE created_by ~ uuid_literal)
    INTO n_stale_authors;

  IF n_stale_authors > 0 THEN
    RAISE WARNING '0015: % author stamp(s) were old local uuids that map to no users.keycloak_id — cleared to NULL rather than left behind as a value that looks like a real subject.', n_stale_authors;
  END IF;

  UPDATE component_configs        SET created_by = NULL WHERE created_by ~ uuid_literal;
  UPDATE component_configs        SET updated_by = NULL WHERE updated_by ~ uuid_literal;
  UPDATE component_config_history SET changed_by = NULL WHERE changed_by ~ uuid_literal;
  UPDATE component_role_bindings  SET granted_by = NULL WHERE granted_by ~ uuid_literal;
  UPDATE pipeline_versions        SET created_by = NULL WHERE created_by ~ uuid_literal;
END $$;

-- ------------------------------------------------------------
-- 阶段 5：删列 + 删表（不可逆）
--   索引随后自动消失（components.owner_user 的 idx_components_owner_user、
--   component_role_bindings.user_id 的索引）。owner_sub 的索引由 AutoMigrate 建。
--   到这一步指向 users 的 FK 已在阶段 0 全部摘除，故 DROP TABLE 不会再被依赖挡住。
-- ------------------------------------------------------------
ALTER TABLE components               DROP COLUMN IF EXISTS owner_user;
ALTER TABLE component_role_bindings  DROP COLUMN IF EXISTS user_id;
ALTER TABLE component_role_bindings  DROP COLUMN IF EXISTS role_id;

DROP TABLE IF EXISTS users;

COMMIT;

-- ------------------------------------------------------------
-- 收尾核对（手工执行，预期：三列都不存在、users 表不存在、owner_sub 在）
-- ------------------------------------------------------------
-- SELECT column_name FROM information_schema.columns
--  WHERE table_name = 'components' AND column_name LIKE 'owner%';
-- SELECT column_name FROM information_schema.columns
--  WHERE table_name = 'component_role_bindings' AND column_name IN ('user_id','role_id','granted_by');
-- SELECT to_regclass('public.users');            -- 预期 NULL
-- SELECT count(*) FROM pg_constraint WHERE contype='f' AND confrelid='users'::regclass;  -- 已无表可查
--
-- ⚠️ 遗留说明：V1 `roles` 表**本批不删**。它已没有任何列引用（role_id 已删），
--    但「删 roles 表」不在已确认的 D3 范围内，属于单独一件事，见
--    plans/UNIMPLEMENTED-MODULES-PLAN.md §14 的「未做/不在范围」小节。
