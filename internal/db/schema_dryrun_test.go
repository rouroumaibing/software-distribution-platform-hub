package db

import (
	"context"
	"strings"
	"testing"
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	componentmodels "github.com/rouroumaibing/software-distribution-platform-hub/internal/component/models"
	permmodels "github.com/rouroumaibing/software-distribution-platform-hub/internal/permission/models"
	pipelinemodels "github.com/rouroumaibing/software-distribution-platform-hub/internal/pipeline/models"
)

// 不连真实库：DryRun 让 GORM 只**生成** SQL 而不执行，用来钉住
// "模型声明 → 期望 DDL" 的对应关系（DELETE-CONTRACT §6.6-3 用的就是这个手法）。
//
// 为什么值得单独测：B-14 / B-15 的核心风险恰恰是"模型标签写错 → AutoMigrate 悄悄
// 建出错误的索引 / 缺列"，而这种错误在没有真实库的环境里平时完全测不出来
//（AutoMigrate 只加不删，不会报错）。写这组断言时它立刻抓到了一个真实缺陷：
// uniqueIndex 标签漏标 participating 字段，会生成 UNIQUE(name, sequence) /
// UNIQUE(name) —— 让阶段名/任务名在**全表**唯一，而不是在 pipeline/stage 内唯一。

// sqlRecorder 通过 GORM 的 logger 捕获 AutoMigrate 生成的语句。
type sqlRecorder struct {
	sql []string
}

func (r *sqlRecorder) LogMode(logger.LogLevel) logger.Interface      { return r }
func (r *sqlRecorder) Info(context.Context, string, ...interface{})  {}
func (r *sqlRecorder) Warn(context.Context, string, ...interface{})  {}
func (r *sqlRecorder) Error(context.Context, string, ...interface{}) {}

func (r *sqlRecorder) Trace(_ context.Context, _ time.Time, fc func() (string, int64), _ error) {
	stmt, _ := fc()
	r.sql = append(r.sql, stmt)
}

func (r *sqlRecorder) joined() string { return strings.Join(r.sql, "\n") }

func openDryRun(t *testing.T) (*sqlRecorder, *gorm.DB) {
	t.Helper()
	rec := &sqlRecorder{}
	gdb, err := gorm.Open(postgres.New(postgres.Config{DSN: "host=127.0.0.1 user=x dbname=x"}), &gorm.Config{
		DryRun:               true,
		DisableAutomaticPing: true,
		Logger:               rec,
	})
	if err != nil {
		t.Fatalf("dry-run gorm open: %v", err)
	}
	return rec, gdb.Session(&gorm.Session{DryRun: true})
}

// createTableStmt 取出某张表的 CREATE TABLE 语句（DryRun 下 information_schema
// 查询返回空，所以 AutoMigrate 走的是"建表"分支而不是"加列"分支）。
func createTableStmt(ddl, table string) string {
	prefix := `CREATE TABLE "` + table + `" (`
	for _, line := range strings.Split(ddl, "\n") {
		if strings.HasPrefix(line, prefix) {
			return line
		}
	}
	return ""
}

// B-15：pipeline_stages / pipeline_task_templates 补 deleted_at，且唯一性改为
// "只作用于未软删行"的 partial unique index —— 与 pipelines 的
// idx_pipelines_component_name_active（0006）同手法。
func TestSchema_StageAndTaskTemplateAreSoftDeleted(t *testing.T) {
	rec, gdb := openDryRun(t)
	if err := gdb.AutoMigrate(&pipelinemodels.PipelineStage{}, &pipelinemodels.PipelineTaskTemplate{}); err != nil {
		t.Fatalf("dry-run AutoMigrate: %v", err)
	}
	ddl := rec.joined()
	t.Logf("generated DDL:\n%s", ddl)

	// 1) 两张表都必须映射 deleted_at（软删），否则模型换 Base 等于没换。
	for _, table := range []string{"pipeline_stages", "pipeline_task_templates"} {
		stmt := createTableStmt(ddl, table)
		if stmt == "" {
			t.Errorf("no CREATE TABLE emitted for %s", table)
			continue
		}
		if !strings.Contains(stmt, `"deleted_at" timestamptz`) {
			t.Errorf("%s must map deleted_at (soft delete), got:\n%s", table, stmt)
		}
	}

	// 2) partial unique index 的**列清单**必须与 0001 的旧约束语义一致：
	//    pipeline_stages = (pipeline_id, sequence)、task_templates = (stage_id, name)。
	//    断言整条语句而不是只断言索引名 —— "索引名对、列错"正是会静默改语义的坑。
	for _, want := range []string{
		`CREATE UNIQUE INDEX IF NOT EXISTS "idx_stages_pipeline_seq_active" ON "pipeline_stages" ("pipeline_id","sequence") WHERE deleted_at IS NULL`,
		`CREATE UNIQUE INDEX IF NOT EXISTS "idx_task_templates_stage_name_active" ON "pipeline_task_templates" ("stage_id","name") WHERE deleted_at IS NULL`,
	} {
		if !strings.Contains(ddl, want) {
			t.Errorf("expected DDL to contain:\n  %s\ngot:\n%s", want, ddl)
		}
	}
}

// B-14：配置审计行新增 environment_key 快照列（FK 的摘除靠 migrations/0008 手跑，
// AutoMigrate 只加列、不删约束）。
func TestSchema_ConfigHistoryCarriesEnvironmentKeySnapshot(t *testing.T) {
	rec, gdb := openDryRun(t)
	if err := gdb.AutoMigrate(&componentmodels.ComponentConfigHistory{}); err != nil {
		t.Fatalf("dry-run AutoMigrate: %v", err)
	}
	ddl := rec.joined()
	t.Logf("generated DDL:\n%s", ddl)

	stmt := createTableStmt(ddl, "component_config_history")
	if stmt == "" {
		t.Fatalf("no CREATE TABLE emitted for component_config_history:\n%s", ddl)
	}
	if !strings.Contains(stmt, `"environment_key" varchar(64)`) {
		t.Errorf("component_config_history must carry the environment_key snapshot, got:\n%s", stmt)
	}
	// environment_id 仍保留为"尽力引用"（只摘约束、不删列）。
	if !strings.Contains(stmt, `"environment_id" uuid`) {
		t.Errorf("environment_id column must be kept (FK dropped, column retained), got:\n%s", stmt)
	}
}

// C-12：pipeline_stages 必须映射 execution_mode（stage 级并行/串行）。
//
// 该字段的**契约与 DDL 草案早已存在**（hub/API-REFERENCE.md 的
// `stages[].executionMode`、hub/DATA-MODEL.md §6.4-① 连 ALTER 都写好了），但
// PipelineStage 结构体长期没有它 —— 缺列的直接后果是 console 编排页的阶段
// 「并行/串行」开关无处落库，只能做成刷新即失效的装饰控件。
//
// 这里钉两点，都是"写错也不报错"的类型：
//   - 列类型必须够宽且 NOT NULL：否则 serial 会被静默截断/存成 NULL；
//   - DEFAULT 必须是 'parallel'：它决定"老客户端不传 executionMode"时落库的值，
//     也是拿不到模型默认值的手工 SQL 路径的唯一防线。
func TestSchema_StageCarriesExecutionMode(t *testing.T) {
	rec, gdb := openDryRun(t)
	if err := gdb.AutoMigrate(&pipelinemodels.PipelineStage{}); err != nil {
		t.Fatalf("dry-run AutoMigrate: %v", err)
	}
	ddl := rec.joined()
	t.Logf("generated DDL:\n%s", ddl)

	stmt := createTableStmt(ddl, "pipeline_stages")
	if stmt == "" {
		t.Fatalf("no CREATE TABLE emitted for pipeline_stages:\n%s", ddl)
	}
	if !strings.Contains(stmt, `"execution_mode" varchar(16) NOT NULL`) {
		t.Errorf("pipeline_stages must map a NOT NULL execution_mode varchar(16), got:\n%s", stmt)
	}
	if !strings.Contains(stmt, `"execution_mode" varchar(16) NOT NULL DEFAULT 'parallel'`) {
		t.Errorf("execution_mode must default to 'parallel' (lowercase, matching the API contract), got:\n%s", stmt)
	}
}

// C-09：pipeline_versions 必须带 (pipeline_id, version) 唯一约束。
//
// 这条断言测的是本功能的**核心不变量**：版本号是面向用户的寻址键（对比与回滚都按
// 号码取快照），允许重复会让"回滚到 v3"变成二义性问题。写错 uniqueIndex 标签
// （例如只标在 Version 上）会生成 `UNIQUE(version)` —— 语义从"某流水线内唯一"
// 静默变成"全表唯一"，第二条流水线的 v1 直接撞键 500。AutoMigrate 只加不删、
// 不会报错，所以只能靠 DryRun 把 DDL 钉住（与 0009 的 partial index 同一手法）。
func TestSchema_PipelineVersionsAreUniquePerPipeline(t *testing.T) {
	rec, gdb := openDryRun(t)
	if err := gdb.AutoMigrate(&pipelinemodels.PipelineVersion{}); err != nil {
		t.Fatalf("dry-run AutoMigrate: %v", err)
	}
	ddl := rec.joined()
	t.Logf("generated DDL:\n%s", ddl)

	stmt := createTableStmt(ddl, "pipeline_versions")
	if stmt == "" {
		t.Fatalf("no CREATE TABLE emitted for pipeline_versions:\n%s", ddl)
	}
	// 快照体必须 NOT NULL：一版没有内容的"版本"在对比/回滚时只会炸在解析上。
	if !strings.Contains(stmt, `"snapshot" JSONB NOT NULL`) {
		t.Errorf("snapshot body must be NOT NULL, got:\n%s", stmt)
	}
	if !strings.Contains(ddl, `CREATE UNIQUE INDEX IF NOT EXISTS "idx_pipeline_versions_pipeline_version" ON "pipeline_versions" ("pipeline_id","version")`) {
		t.Errorf("expected the (pipeline_id, version) unique index, got:\n%s", ddl)
	}
	// 反向断言：不允许出现只按 version 的唯一索引（那会是"全表唯一"的静默语义漂移）。
	if strings.Contains(ddl, `ON "pipeline_versions" ("version")`) {
		t.Errorf("version must NOT be globally unique — it is unique per pipeline, got:\n%s", ddl)
	}
}

// B-11 自定义角色：component_roles 的唯一性必须是两条 **partial** index。
//
// 这里钉的是一个"写错也完全不报错"的坑：若图省事写成 `unique (org_id, name)`，
// **内置角色（org_id is null）会完全不受约束** —— Postgres 的 unique 里 NULL 互不
// 相等，多条 `(null, 'component-admin')` 都能插入。于是 `GetByName("component-admin")`
// （owner 引导、绑定校验都走它）开始返回不确定的一行，权限判定随查询计划漂移。
// 所以必须有 `where org_id is null` 那条把内置角色名钉成全局唯一。
func TestSchema_ComponentRolesAreUniquePerOrg(t *testing.T) {
	rec, gdb := openDryRun(t)
	if err := gdb.AutoMigrate(&permmodels.ComponentRole{}); err != nil {
		t.Fatalf("dry-run AutoMigrate: %v", err)
	}
	ddl := rec.joined()
	t.Logf("generated DDL:\n%s", ddl)

	if createTableStmt(ddl, "component_roles") == "" {
		t.Fatalf("no CREATE TABLE emitted for component_roles:\n%s", ddl)
	}
	for _, want := range []string{
		`CREATE UNIQUE INDEX IF NOT EXISTS "idx_component_roles_org_name" ON "component_roles" ("org_id","name") WHERE org_id IS NOT NULL`,
		`CREATE UNIQUE INDEX IF NOT EXISTS "idx_component_roles_builtin_name" ON "component_roles" ("name") WHERE org_id IS NULL`,
	} {
		if !strings.Contains(ddl, want) {
			t.Errorf("expected DDL to contain:\n  %s\ngot:\n%s", want, ddl)
		}
	}
	// 反向断言：内置角色的唯一性不能"掉进" NULL 语义里 —— 缺 where 子句就说明
	// 生成的是普通 (org_id, name) 唯一，对 org_id is null 完全无效。
	if strings.Contains(ddl, `ON "component_roles" ("org_id","name")`+"\n") {
		t.Errorf("(org_id, name) unique without a WHERE clause does not constrain built-ins (NULLs are distinct), got:\n%s", ddl)
	}
}
