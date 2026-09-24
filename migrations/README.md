# migrations/ —— 真实契约（2026-09-23 用真实 Postgres 16 实测后重写）

本目录**不是**"从零建库的完整 schema"，也**不是**可以无脑顺序重放的脚本集合。
在真库上实测之前，这里只有承诺、没有验证；实测推翻了三条承诺，故重写本文。

---

## 1. 一句话契约

> **建表靠 GORM `AutoMigrate`（`internal/db/db.go`，SSOT）；本目录只负责 AutoMigrate
> 做不到的事** —— 删列 / 删表 / 改类型 / 改名 / 换约束形状 / 数据回填。

AutoMigrate 的特性决定了这条分工：**它只加不删**（不删列、不删约束、不改名），
所以"把已经不用的东西去掉"永远需要手写 SQL。

---

## 2. 两段集合，职责不同（别再混为一谈）

| 集合 | 文件 | 定位 | 能否在 AutoMigrate 建出的库上跑 |
|---|---|---|---|
| **引导集合** | `0001`–`0005` | 历史：P0 时代"用 SQL 从零建库"的产物 | ❌ **不能**。会报 `orgs already exists`、`column env_type already exists` 等 |
| **运维集合** | `0006`–`0016` | 现行：在 AutoMigrate 之后应用的增量变更 | ✅ 可以，且**要求**先 AutoMigrate |

实测记录（2026-09-23，PG 16.15）：

```
在 AutoMigrate 建出的库上顺序重放 0001..0016：
  0001 FAIL relation "orgs" already exists
  0002 FAIL column "env_type" of relation "environments" already exists
  0003 FAIL relation "users" does not exist       ← 0003 建的是 users，模型已移除
  0004 FAIL column crb.user_id does not exist
  0005 FAIL column "user_id" of relation "component_role_bindings" does not exist
  0006..0014 OK
  0015 FAIL column "owner_user" does not exist    ← 已修，见 §5
  0016 OK
```

⇒ **`0001`–`0005` 不是"新装路径"，是历史遗迹。** 全新安装请直接用 AutoMigrate。

---

## 3. 两条受支持的路径

### Path A —— 全新安装（= `deploy-local.sh` 的实际路径）

```
起 Postgres ──► 启动 hub（AutoMigrate 建 31 张表）──► psql 0006..0016（全部空操作）
                                                            └─ 也可跑 cmd/hub/conf/import.sh 灌演示数据
```

`deploy-local.sh` 本身**不跑 migrations**：全新库里 AutoMigrate 已经给出正确 schema，
`0006`–`0016` 只是幂等的收敛脚本。

### Path B —— 老库升级（已上线环境升到新版）

```
备份 pg_dump ──► 停旧 hub ──► psql 0015 ──► 部署新版 hub（AutoMigrate）──► psql 0006..0014,0016
                                 │
                                 └─ ⚠️ 0015 必须**先于**新版 hub 启动，见 §4
```

---

## 4. ⚠️ 顺序铁律：`0015` 必须先于新版 hub 启动（与早先注释相反）

`0015` 的文件头曾写"1) 先部署新版 hub，2) 再跑 0015"。**实测证明这在老库上不可行**：

```
老库 + 新版 hub 启动 → AutoMigrate
  ERROR: foreign key constraint "component_configs_created_by_fkey" cannot be implemented (42804)
  F hub: failed to open db
```

原因：AutoMigrate 要把 `component_configs.created_by` 从 `uuid` 改成 `varchar(128)`
（模型已是 `string`），而指向 `users(id)` 的 FK 还在 —— Postgres 拒绝改类型。**AutoMigrate
自己也撞同一堵墙**，所以"先上新版"这一步根本走不到 `0015`。

`0015` 自带 `ADD COLUMN IF NOT EXISTS owner_sub`，**不依赖 AutoMigrate 先跑**，所以正确顺序是
**先 `0015` 摘 FK + 回填 + 删表，再上新版**。

---

## 5. 已修的真实缺陷（都是"只在真库上才暴露"）

| 缺陷 | 症状 | 修法 |
|---|---|---|
| `0015` 未摘 FK 就改列类型 | `42804 cannot be implemented`；且后续 `DROP TABLE users` 也必被依赖挡住 | 阶段 0 用 `pg_constraint` **动态枚举**并摘除全部入向 FK（写死清单会随模型演进过期），实测摘掉 **6** 条 |
| `0015` 阶段 4 无条件引用 `owner_user` | 全新安装路径直接崩 `column "owner_user" does not exist`；"全部语句带 IF EXISTS"这句承诺当时是假的 | 只在该列仍存在时执行（`information_schema` 判断） |
| `0015` 漏掉 `pipeline_versions.created_by` | 同类站点第 7 处：模型早已是 `string`，老库仍是 `uuid` + FK | 一并转类型 + 回填 + 清残 |
| `0001` 用内联 `unique` 建唯一约束 | 老库上 AutoMigrate 崩：`constraint "uni_orgs_slug" of relation "orgs" does not exist`，**hub 完全起不来** | `orgs.slug` / `service_trees.org_id` / `targets.name` 三处改为显式命名唯一索引 `idx_<table>_<col>`，与模型 `uniqueIndex` 对齐 |

**`0001` 那条的机理**（值得记住）：内联 `unique` 建出的是 UNIQUE **约束**；模型写 `uniqueIndex`
建出的是**索引**。GORM 的 `MigrateColumnUnique` 逐列判断，发现"库里有唯一约束、模型不要"
就去 `DropConstraint(uni_<table>_<col>)` —— 而实际的约束叫 `<table>_<col>_key`，名字对不上，
直接 42704 崩掉整个 AutoMigrate。

**注意**：该形状分歧**只存在于 `0001` 引导出的库**。已核实真实旧库（`pg_dump` 快照）里是
`idx_orgs_slug`（索引形态），**没有任何 `uni_*` 约束** —— 即真实环境都是 AutoMigrate 建的，
不受此影响。修 `0001` 是为了让"重放引导脚本"不再产出 AutoMigrate 无法接管的库。

---

## 6. 验证：`hack/migration-check.sh`

```bash
bash hack/migration-check.sh      # 需要 docker + go；不依赖本机 psql
```

用一次性 Postgres 容器把 Path A 与 Path B 都跑一遍，并断言：

- 老库回填正确（`owner_sub` / `created_by` / `updated_by` / `pipeline_versions.created_by` → `kc-sub-alice`）
- `users` 表已删、无残留入向 FK
- 两条路径的**列集合差异必须全部落在已知白名单内**，出现新差异直接 FAIL

**这是迁移类改动的前置门禁。** `go build` / `go vet` / `go test` / GORM `DryRun`
**看不到**任何上述缺陷 —— DryRun 只捕获 SQL 文本，不碰 Postgres。

### 已知且良性的两路径差异（白名单，勿当噪声）

| 差异 | 原因 | 为何良性 |
|---|---|---|
| `rollout_runs.created_at` / `updated_at`（仅老库有） | 模型 `internal/run/models/rollout_run.go` **没有**嵌入时间戳基类，AutoMigrate 不建这两列；`0001` 建了 | 老库里是 `not null default now()` ⇒ GORM 的 INSERT 不带它们也由 DB 默认值填上，**不会插入失败** |
| `pipeline_task_templates.environment_id`（仅老库有） | `0001` 的 Deploy 任务专属列；模型已无 `EnvironmentID` 字段 | 可空、无读写方的 inert 残留 |

这两条都是"AutoMigrate 只加不删"的必然结果。**不通过再写一个删列迁移去"修平"** ——
删列不可逆、且对功能零收益。

---

## 7. 新增迁移的规矩

1. **文件名**：`NNNN_<snake_case_描述>.sql`，四位零填充、**单调递增**，不插队。
2. **必须幂等**：`IF EXISTS` / `IF NOT EXISTS` / `to_regclass()` 守卫；重复执行是空操作。
   若引用了一张**由 AutoMigrate 建的**表，而该表在引导库里不存在，请显式写前置断言
   （`RAISE EXCEPTION` 带可操作提示），**不要**用 `if exists ... end if` 静默跳过 ——
   静默跳过会让 schema 变得"静默地不对"，比报错危险。
3. **必须包事务**：`BEGIN; ... COMMIT;`（本目录已有反例见 `0015`，其余按惯例）。
4. **不可逆动作要先备份**：文件头写明 `pg_dump` 命令。
5. **改完跑 `bash hack/migration-check.sh`**，并把新增的两路径差异（若有）登记进白名单 + 本文。
6. **不要往这里放数据**：演示/参考数据一律进 `cmd/hub/conf/*.sql`（配合 `import.sh`，dev-only、幂等）。
   理由：`migrations/` 会进生产全新安装路径，塞演示行会污染生产库。
