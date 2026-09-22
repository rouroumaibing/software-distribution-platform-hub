package repository

import (
	"context"
	"regexp"
	"strings"
	"testing"
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// 不连真实库：DryRun 只**生成** SQL。这组断言钉住 IDsBySlugs 的两个隐含前提
// ——它是 token 的组织载体（`/org:<slug>`）与 resource_ownership 的 uuid 列之间
// 唯一的连接点，写错不会有任何报错，只会让归属判定永远查不到组织：
//
//  1. 别名必须在**一条** `slug IN (...)` 查询里批量解析（N 个别名 = 1 次往返，
//     不是 N 次），否则每个请求都会随组织数线性变慢；
//  2. 必须带 `deleted_at IS NULL`（Org 嵌 common.Base）：软删过的组织若能解析
//     出来，token 里一个已废弃的别名就能把访问权重新激活。

type captureLogger struct{ sql []string }

func (l *captureLogger) LogMode(logger.LogLevel) logger.Interface      { return l }
func (l *captureLogger) Info(context.Context, string, ...interface{})  {}
func (l *captureLogger) Warn(context.Context, string, ...interface{})  {}
func (l *captureLogger) Error(context.Context, string, ...interface{}) {}

func (l *captureLogger) Trace(_ context.Context, _ time.Time, fc func() (string, int64), _ error) {
	stmt, _ := fc()
	l.sql = append(l.sql, stmt)
}

func openOrgDryRun(t *testing.T) (*captureLogger, *gorm.DB) {
	t.Helper()
	rec := &captureLogger{}
	gdb, err := gorm.Open(postgres.New(postgres.Config{DSN: "host=127.0.0.1 user=x dbname=x"}), &gorm.Config{
		DryRun:               true,
		DisableAutomaticPing: true,
		Logger:               rec,
	})
	if err != nil {
		t.Fatalf("dry-run gorm open: %v", err)
	}
	return rec, gdb
}

func TestIDsBySlugs_QueryShape(t *testing.T) {
	rec, gdb := openOrgDryRun(t)
	repo := NewOrgRepository(gdb)

	got, err := repo.IDsBySlugs([]string{"team-a", "team-b"})
	if err != nil {
		t.Fatalf("IDsBySlugs: %v", err)
	}
	// DryRun never returns rows — the point is the statement, and an empty map
	// is the honest result (no org is ever invented).
	if len(got) != 0 {
		t.Errorf("dry-run returned %d orgs, want none", len(got))
	}

	sql := strings.Join(rec.sql, "\n")
	for _, want := range []string{`FROM "orgs"`, "slug IN"} {
		if !strings.Contains(sql, want) {
			t.Errorf("generated SQL is missing %q:\n%s", want, sql)
		}
	}
	// 声明式正则：GORM 会把列名加引号（`"orgs"."deleted_at"`），写死
	// `deleted_at IS NULL` 会漏判（正是这组断言第一版踩的坑）。
	softDelete := regexp.MustCompile(`deleted_at"?\s+IS\s+NULL`)
	if !softDelete.MatchString(sql) {
		t.Errorf("soft-deleted orgs would still resolve — missing deleted_at filter:\n%s", sql)
	}
	if strings.Count(sql, "SELECT") != 1 {
		t.Errorf("expected exactly one query for all aliases, got %d:\n%s", strings.Count(sql, "SELECT"), sql)
	}
}

func TestIDsBySlugs_NoAliasesRunsNoQuery(t *testing.T) {
	rec, gdb := openOrgDryRun(t)
	repo := NewOrgRepository(gdb)

	got, err := repo.IDsBySlugs(nil)
	if err != nil {
		t.Fatalf("IDsBySlugs(nil): %v", err)
	}
	if len(got) != 0 {
		t.Errorf("got %d entries, want none", len(got))
	}
	if len(rec.sql) != 0 {
		t.Errorf("an empty alias list must not hit the database, got:\n%s", strings.Join(rec.sql, "\n"))
	}
}
