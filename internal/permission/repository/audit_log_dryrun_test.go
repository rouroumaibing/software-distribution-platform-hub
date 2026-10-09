package repository

import (
	"strings"
	"testing"
	"time"
)

// 审计查询的**查询形状**回归测试（与 platform_binding_dryrun_test.go 同源
// 手法：DryRun 只生成 SQL 不执行，语句经 logger 捕获）。
//
// 为什么值得测 —— 这里钉的是"过滤条件写错也不报错"的一类静默 bug：
// 审计是事后追责的唯一依据，WHERE 条件比预期宽（如 subject 过滤丢失）时，
// 查"某人干了什么"会返回所有人的记录 —— 页面上看起来"能跑"，但审计语义
// 已经错了。所以每条过滤、排序方向、limit 钳制都逐条钉住。

func TestAuditQuery_FiltersShape(t *testing.T) {
	rec, gdb := openDryRun(t)
	repo := NewAuditLogRepository(gdb)

	since := time.Date(2026, 10, 8, 0, 0, 0, 0, time.UTC)
	until := since.Add(24 * time.Hour)

	if _, err := repo.Query(AuditQuery{
		Subject:      "user-abc",
		ActionPrefix: "approval.",
		ResourceType: "component",
		ResourceID:   "comp-1",
		Since:        since,
		Until:        until,
		Limit:        50,
	}); err != nil {
		t.Fatalf("query: %v", err)
	}

	sql := rec.joined()
	for _, want := range []string{
		"subject =",
		"action LIKE",
		"resource_type =",
		"resource_id =",
		"timestamp >=",
		"timestamp <=",
		"ORDER BY timestamp DESC",
	} {
		if !strings.Contains(sql, want) {
			t.Errorf("SQL 缺少过滤/排序片段 %q\n完整 SQL:\n%s", want, sql)
		}
	}
}

// 空 query 必须退化为「最新 N 条」：不带任何 WHERE，但仍按时间倒序。
func TestAuditQuery_EmptyIsLatestFirst(t *testing.T) {
	rec, gdb := openDryRun(t)
	repo := NewAuditLogRepository(gdb)

	if _, err := repo.Query(AuditQuery{}); err != nil {
		t.Fatalf("query: %v", err)
	}

	sql := rec.joined()
	if strings.Contains(sql, "WHERE") {
		t.Errorf("空 query 不应产生 WHERE 子句\nSQL: %s", sql)
	}
	if !strings.Contains(sql, "ORDER BY timestamp DESC") {
		t.Errorf("缺少时间倒序\nSQL: %s", sql)
	}
}

// limit 钳制在仓储层：非法/超大值不许透传到 SQL。
func TestAuditQuery_LimitClamped(t *testing.T) {
	cases := []struct {
		name  string
		in    int
		want  string
		notIn string
	}{
		{"零值回默认 100", 0, "LIMIT 100", "LIMIT 0"},
		{"负值回默认 100", -5, "LIMIT 100", "LIMIT -5"},
		{"超上限钳到 500", 9999, "LIMIT 500", "LIMIT 9999"},
		{"合法值原样", 42, "LIMIT 42", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec, gdb := openDryRun(t)
			repo := NewAuditLogRepository(gdb)
			if _, err := repo.Query(AuditQuery{Limit: tc.in}); err != nil {
				t.Fatalf("query: %v", err)
			}
			sql := rec.joined()
			if !strings.Contains(sql, tc.want) {
				t.Errorf("SQL 应含 %q\nSQL: %s", tc.want, sql)
			}
			if tc.notIn != "" && strings.Contains(sql, tc.notIn) {
				t.Errorf("SQL 不应含 %q\nSQL: %s", tc.notIn, sql)
			}
		})
	}
}
