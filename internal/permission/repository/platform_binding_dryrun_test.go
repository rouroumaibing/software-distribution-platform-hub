package repository

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// 不连真实库的**查询形状**回归测试（与 internal/db/schema_dryrun_test.go、
// internal/search/repository/search_dryrun_test.go 同源手法：DryRun 让 GORM
// 只生成 SQL 不执行，语句经 logger 捕获）。
//
// 为什么值得单独测 —— 这里钉的是"写错也不报错"的一类 bug：
//
//	expires_at 是 §7.4/§10 #15 引入的 TTL。若 ListMatching 忘了加
//	`expires_at IS NULL OR expires_at > now()`，代码照样编译、照样跑通、
//	单测照样绿，但**已过期的授权仍然生效** —— 权限系统里最危险的那种静默错误。
//	这是唯一被鉴权中间件使用的解析路径，所以过滤必须长在这里，而不能只在
//	API 边界或某个 handler 里做。
//
//	反方向也要钉：管理面用的 List 必须**不过滤**过期行，否则管理员看不到
//	（也就无法清理）已经失效的绑定。

type sqlRecorder struct{ sql []string }

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

const expiryClause = "expires_at IS NULL OR expires_at >"

func TestPlatformListMatching_ExcludesExpiredGrants(t *testing.T) {
	rec, gdb := openDryRun(t)
	repo := NewPlatformRoleBindingRepository(gdb)

	org := uuid.New()
	if _, err := repo.ListMatching(&org, "sub-1", []string{"/sdp-admin"}); err != nil {
		t.Fatalf("ListMatching: %v", err)
	}
	sql := rec.joined()
	t.Logf("SQL: %s", sql)

	if !strings.Contains(sql, "platform_role_bindings") {
		t.Fatalf("查错了表:\n%s", sql)
	}
	if !strings.Contains(sql, expiryClause) {
		t.Fatalf("缺少到期过滤 —— 已过期的授权会继续生效:\n%s", sql)
	}
	// 主体条件必须同时在：user 分支（sub）与 group 分支（claim 逐字）。
	if !strings.Contains(sql, "subject_type") || !strings.Contains(sql, "subject_id") {
		t.Fatalf("缺少主体条件:\n%s", sql)
	}
	if !strings.Contains(sql, "IN") {
		t.Fatalf("传入 groups 时应走 IN 分支:\n%s", sql)
	}
	// org 维度：org-scoped 绑定只在 orgID 非空且匹配时纳入。
	if !strings.Contains(sql, "org_id") {
		t.Fatalf("缺少 org 隔离条件:\n%s", sql)
	}
}

func TestPlatformListMatching_OrgNilStaysGlobalOnly(t *testing.T) {
	// orgID 为 nil 时只能取全局绑定（org_id IS NULL）；退化成正作用域查询
	// 会让某个组织的绑定泄漏给所有人。
	rec, gdb := openDryRun(t)
	repo := NewPlatformRoleBindingRepository(gdb)

	if _, err := repo.ListMatching(nil, "sub-1", nil); err != nil {
		t.Fatalf("ListMatching: %v", err)
	}
	sql := rec.joined()
	t.Logf("SQL: %s", sql)

	if !strings.Contains(sql, "org_id IS NULL") {
		t.Fatalf("orgID 为 nil 时应收窄到全局绑定:\n%s", sql)
	}
	if strings.Contains(sql, "IN (") {
		t.Fatalf("空 groups 不应生成 IN 分支:\n%s", sql)
	}
	if !strings.Contains(sql, expiryClause) {
		t.Fatalf("缺少到期过滤:\n%s", sql)
	}
}

func TestPlatformList_KeepsExpiredForManagementView(t *testing.T) {
	// 反方向：管理面必须能看到已失效的绑定，否则无法清理，也无从解释
	// "这条为什么不再生效"。
	rec, gdb := openDryRun(t)
	repo := NewPlatformRoleBindingRepository(gdb)

	org := uuid.New()
	if _, err := repo.List(&org, "group", "/sdp-admin"); err != nil {
		t.Fatalf("List: %v", err)
	}
	sql := rec.joined()
	t.Logf("SQL: %s", sql)

	if strings.Contains(sql, "expires_at") {
		t.Fatalf("管理列表不应过滤 expires_at（否则看不到已过期的绑定）:\n%s", sql)
	}
	if !strings.Contains(sql, "ORDER BY") {
		t.Fatalf("列表应稳定排序（created_at DESC）:\n%s", sql)
	}
}

func TestPlatformExistsActive_ExcludesExpiredGrants(t *testing.T) {
	// 重复校验也必须只看**仍生效**的绑定：否则一条今天过期的绑定会永久
	// 挡住重新授予（用户被锁在"申请—过期—无法再申请"的循环里）。
	rec, gdb := openDryRun(t)
	repo := NewPlatformRoleBindingRepository(gdb)

	if _, err := repo.ExistsActive(nil, "group", "/sdp-admin", uuid.New()); err != nil {
		t.Fatalf("ExistsActive: %v", err)
	}
	sql := rec.joined()
	t.Logf("SQL: %s", sql)

	if !strings.Contains(sql, "count(*)") {
		t.Fatalf("重复校验应走 count(*):\n%s", sql)
	}
	if !strings.Contains(sql, expiryClause) {
		t.Fatalf("重复校验未排除过期绑定:\n%s", sql)
	}
	if !strings.Contains(sql, "platform_role_id") {
		t.Fatalf("重复校验必须带上角色维度:\n%s", sql)
	}
}

func TestComponentListMatching_ExcludesExpiredGrants(t *testing.T) {
	// component_role_bindings 与 platform 侧同批加 expires_at，故同一风险：
	// 漏过滤 = 过期的组件级授权继续生效。
	rec, gdb := openDryRun(t)
	repo := NewBindingRepository(gdb)

	if _, err := repo.ListMatching(uuid.New(), "sub-1", []string{"/sdp-admin"}); err != nil {
		t.Fatalf("ListMatching: %v", err)
	}
	sql := rec.joined()
	t.Logf("SQL: %s", sql)

	if !strings.Contains(sql, "component_role_bindings") {
		t.Fatalf("查错了表:\n%s", sql)
	}
	if !strings.Contains(sql, expiryClause) {
		t.Fatalf("缺少到期过滤 —— 已过期的组件级授权会继续生效:\n%s", sql)
	}
	// D3 反向断言：V1 遗留分支（user_id）已随列一起删除。它若重新出现在 SQL
	// 里，说明有人把「按本地 users.id 兜底」写了回来 —— 那张表已经不存在，
	// 查询会直接报错而不是静默变松，所以这条断言要一直在。
	if strings.Contains(sql, "user_id") {
		t.Fatalf("V1 遗留 user_id 分支仍在生成（该列已随 D3 删除）:\n%s", sql)
	}
}
