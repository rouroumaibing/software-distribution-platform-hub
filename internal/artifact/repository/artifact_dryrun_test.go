package repository

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// 不连真实库的**查询形状**回归测试（与 internal/db/schema_dryrun_test.go、
// internal/search/repository/search_dryrun_test.go 同源手法：DryRun 只生成 SQL 不执行）。
//
// 这里钉的是 B-16 收口后最容易被"顺手统一"掉的一点：
// **列表查询与 GC 查询必须用不同的谓词**。
//   - 列表隐藏 `pending_deletion`（逻辑已删，不该再提供下载）；
//   - GC 反过来**必须包含** `pending_deletion`（那是上一轮对象删失败的行，本轮要重试）。
//
// 两个谓词一旦被统一（复制粘贴时最容易发生），后果都是静默的：要么列表露出正在被
// 回收的制品，要么重试永远不发生、失败的行永久滞留。

func openDryRun(t *testing.T) *gorm.DB {
	t.Helper()
	gdb, err := gorm.Open(postgres.New(postgres.Config{DSN: "host=127.0.0.1 user=x dbname=x"}), &gorm.Config{
		DryRun:               true,
		DisableAutomaticPing: true,
	})
	if err != nil {
		t.Fatalf("dry-run gorm open: %v", err)
	}
	return gdb.Session(&gorm.Session{DryRun: true})
}

// builtSQL 触发一次 finisher 让 GORM 生成 SQL，并返回语句与绑定参数。
func builtSQL(t *testing.T, tx *gorm.DB) (string, []any) {
	t.Helper()
	var dest []map[string]any
	if err := tx.Find(&dest).Error; err != nil {
		t.Fatalf("dry-run Find: %v", err)
	}
	return tx.Statement.SQL.String(), tx.Statement.Vars
}

func varsContain[T comparable](vars []any, want T) bool {
	for _, v := range vars {
		if v == any(want) {
			return true
		}
	}
	return false
}

func TestQueryShape_ListHidesPendingDeletion(t *testing.T) {
	sql, vars := builtSQL(t, liveForComponent(openDryRun(t), uuid.Nil))
	t.Logf("SQL: %s\nVARS: %v", sql, vars)

	if !strings.Contains(sql, "cleanup_state") {
		t.Fatalf("列表查询必须过滤 cleanup_state，否则会把正在回收的制品列出来:\n%s", sql)
	}
	if !varsContain(vars, "pending_deletion") {
		t.Fatalf("列表查询应排除 pending_deletion，实际参数 %v", vars)
	}
	if !strings.Contains(sql, "component_id = ") {
		t.Fatalf("列表查询必须锚定 component_id:\n%s", sql)
	}
	// created_at desc 是列表的既有排序契约，别在改动中丢掉。
	if !strings.Contains(sql, "ORDER BY created_at desc") {
		t.Fatalf("列表查询必须按 created_at 倒序:\n%s", sql)
	}
}

func TestQueryShape_ExpiredIncludesPendingDeletion(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	repo := NewArtifactRepository(openDryRun(t))
	sql, vars := builtSQL(t, repo.expiredQuery(now, 25))
	t.Logf("SQL: %s\nVARS: %v", sql, vars)

	// 1) 过期判定必须真的落在 SQL 上（这是 `expires_at` 唯一的读取点）。
	if !strings.Contains(sql, "expires_at IS NOT NULL") || !strings.Contains(sql, "expires_at < ") {
		t.Fatalf("GC 查询必须表达「非空且已过期」:\n%s", sql)
	}
	if !varsContain(vars, now) {
		t.Fatalf("时钟必须作为参数绑定（便于测试与可复现），实际 %v", vars)
	}

	// 2) **最关键**：绝不能排除 pending_deletion —— 排除它重试就永不发生。
	if strings.Contains(sql, "cleanup_state") {
		t.Fatalf("GC 查询不得过滤 cleanup_state（那会跳过需要重试的行，使 cleanup_state 变成永久滞留）:\n%s", sql)
	}

	// 3) oldest-first：先回收最久远的，避免一批大 backlog 里老行饿死。
	if !strings.Contains(sql, "ORDER BY expires_at asc") {
		t.Fatalf("GC 查询必须按 expires_at 升序:\n%s", sql)
	}

	// 4) 批量闸门必须落到 SQL。
	if !strings.Contains(sql, "LIMIT") || !varsContain(vars, 25) {
		t.Fatalf("limit=25 必须绑定进 SQL（无界的 GC 会退化成删全表），vars=%v\n%s", vars, sql)
	}
}

// limit<=0 时不加 LIMIT（由调用方负责；GC 永远传默认批量，所以这条只护边界）。
func TestQueryShape_ExpiredWithoutLimitHasNoLimitClause(t *testing.T) {
	repo := NewArtifactRepository(openDryRun(t))
	sql, _ := builtSQL(t, repo.expiredQuery(time.Now(), 0))
	if strings.Contains(sql, "LIMIT") {
		t.Fatalf("limit<=0 表示不加闸门，SQL 里不该出现 LIMIT:\n%s", sql)
	}
}

// 两个谓词必须**不同**：这是本批最容易在重构中被抹平的差异。
func TestQueryShape_ListAndGCUseDifferentPredicates(t *testing.T) {
	listSQL, _ := builtSQL(t, liveForComponent(openDryRun(t), uuid.Nil))
	gcRepo := NewArtifactRepository(openDryRun(t))
	gcSQL, _ := builtSQL(t, gcRepo.expiredQuery(time.Now(), 10))

	listFilters := strings.Contains(listSQL, "cleanup_state")
	gcFilters := strings.Contains(gcSQL, "cleanup_state")
	if !listFilters || gcFilters {
		t.Fatalf("谓词必须不同：列表要过滤 cleanup_state(%v)，GC 不能过滤(%v)", listFilters, gcFilters)
	}
}
