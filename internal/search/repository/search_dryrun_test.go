package repository

import (
	"fmt"
	"strings"
	"testing"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// 不连真实库的**查询形状**回归测试（与 internal/db/schema_dryrun_test.go 同源手法：
// DryRun 让 GORM 只生成 SQL 不执行）。
//
// 为什么值得单独测 —— 这里钉的三件事全是"写错也不报错"的类型：
//  1. ORDER BY 到底有没有出现。GORM 的 Order() 只认 string / clause.OrderBy /
//     clause.OrderByColumn，传 gorm.Expr 会被**静默丢弃**：不报错、SQL 里就是没有
//     ORDER BY，结果变成"数据库返回顺序"（实践上≈随机），搜索第一行不再是精确命中。
//  2. LIKE 元字符有没有转义。没转义时用户输入一个 `_` 就命中全表，而测试环境里
//     根本看不出异常。
//  3. 软删过滤是否漏表。orgs / services / components / pipelines 都是软删表，
//     用 Table()+Joins() 时 GORM 的自动条件**不会**覆盖 JOIN 进来的表；漏一个就会
//     把已删资源搜出来。反过来 service_trees 没有 deleted_at 列，对它加过滤会直接
//     SQL 报错，所以它必须**不在**过滤名单里（两个方向都要钉）。

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

// builtSQL 触发一次 finisher 让 GORM 生成 SQL（DryRun 下不真的查库），
// 再把语句与绑定参数一起返回。
func builtSQL(t *testing.T, tx *gorm.DB) (string, []any) {
	t.Helper()
	var dest []map[string]any
	if err := tx.Find(&dest).Error; err != nil {
		t.Fatalf("dry-run Find: %v", err)
	}
	return tx.Statement.SQL.String(), tx.Statement.Vars
}

// varsContain reports whether any bound parameter equals want. 泛型是为了同时
// 覆盖字符串模式（LIKE 参数）与整型（LIMIT）：GORM 直接把 limit 当 int 绑定。
func varsContain[T comparable](vars []any, want T) bool {
	for _, v := range vars {
		if v == any(want) {
			return true
		}
	}
	return false
}

func TestQueryShape_AllThreeJoinToOrgAndFilterSoftDeletedTables(t *testing.T) {
	repo := NewSearchRepository(openDryRun(t))

	cases := []struct {
		name           string
		build          func() *gorm.DB
		softDeleteTabs int // 该查询里必须出现 `deleted_at IS NULL` 的表数
	}{
		{"services", func() *gorm.DB { return repo.servicesQuery("web", 5) }, 2},
		{"components", func() *gorm.DB { return repo.componentsQuery("web", 5) }, 3},
		{"pipelines", func() *gorm.DB { return repo.pipelinesQuery("web", 5) }, 4},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sql, vars := builtSQL(t, tc.build())
			t.Logf("SQL: %s\nVARS: %v", sql, vars)

			// 1) 相关度排序必须真的出现。
			if !strings.Contains(sql, "ORDER BY") {
				t.Fatalf("ORDER BY 缺失 —— clause.OrderBy 没生效（大概率是被换成了 gorm.Expr）:\n%s", sql)
			}
			if !strings.Contains(sql, "CASE WHEN lower(") {
				t.Fatalf("ORDER BY 里没有精确命中优先的 CASE 表达式:\n%s", sql)
			}

			// 2) 大小写不敏感匹配。
			if !strings.Contains(sql, "ILIKE") {
				t.Fatalf("应使用 ILIKE 做大小写不敏感匹配:\n%s", sql)
			}

			// 3) 软删过滤：数量必须刚好，且 service_trees 不参与（它没有该列）。
			if got := strings.Count(sql, "deleted_at IS NULL"); got != tc.softDeleteTabs {
				t.Fatalf("deleted_at IS NULL 出现 %d 次，期望 %d 次（漏了就会搜到已删资源）:\n%s",
					got, tc.softDeleteTabs, sql)
			}
			if strings.Contains(sql, "st.deleted_at") {
				t.Fatalf("service_trees 没有 deleted_at 列，对它加过滤会直接 SQL 报错:\n%s", sql)
			}

			// 4) 每层 JOIN 都在：组织名是 Path 的来源，缺一层路径就拼不出来。
			if !strings.Contains(sql, "service_trees AS st") || !strings.Contains(sql, "orgs AS o") {
				t.Fatalf("缺少 org / service_tree 的 JOIN，无法拼出结果路径:\n%s", sql)
			}

			// 5) limit 必须落到 SQL 上（端点无分页，limit 是唯一的体积闸门）。
			if !strings.Contains(sql, "LIMIT") {
				t.Fatalf("LIMIT 缺失，无分页端点会退化成全表扫描:\n%s", sql)
			}
			if !varsContain(vars, 5) {
				t.Fatalf("limit=5 未绑定进参数，vars=%v", vars)
			}
		})
	}
}

func TestQueryShape_EscapesLikeMetacharacters(t *testing.T) {
	repo := NewSearchRepository(openDryRun(t))

	// 下划线是单字符通配符、百分号是任意串：不转义时搜 "a_b" 会命中 "axb"，
	// 只输入一个 "_" 更是命中全表。
	const raw = `a_b%`
	for _, tc := range []struct {
		name  string
		build func() *gorm.DB
	}{
		{"services", func() *gorm.DB { return repo.servicesQuery(raw, 5) }},
		{"components", func() *gorm.DB { return repo.componentsQuery(raw, 5) }},
		{"pipelines", func() *gorm.DB { return repo.pipelinesQuery(raw, 5) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, vars := builtSQL(t, tc.build())
			t.Logf("VARS: %v", vars)

			wantContains := `%a\_b\%%`
			if !varsContain(vars, wantContains) {
				t.Fatalf("包含模式未转义，期望参数里有 %q，实际 %v", wantContains, vars)
			}
			if varsContain(vars, `%a_b%%`) {
				t.Fatalf("出现了未转义的包含模式 %q —— LIKE 元字符逃逸了: %v", `%a_b%%`, vars)
			}
			// 前缀模式（给 ORDER BY 用）同样要转义。
			if !varsContain(vars, `a\_b\%%`) {
				t.Fatalf("前缀模式未转义，实际 %v", vars)
			}
			// 原样保留一份未转义的 q，供 ORDER BY 做 `lower(name) = lower(?)` 精确比较。
			if !varsContain(vars, raw) {
				t.Fatalf("精确命中比较需要原始 q，实际 %v", vars)
			}
		})
	}
}

func TestQueryShape_PipelinesDoNotMatchOnComponentName(t *testing.T) {
	repo := NewSearchRepository(openDryRun(t))
	sql, _ := builtSQL(t, repo.pipelinesQuery("web", 5))

	// 这是刻意的：让组件名进匹配会让"搜组件"连带命中它名下全部流水线，
	// 把真正的组件命中挤出前 20 条。组件名只能出现在路径（SELECT/JOIN）里。
	if strings.Contains(sql, "c.name ILIKE") {
		t.Fatalf("流水线不应按组件名匹配（会把组件命中挤掉）:\n%s", sql)
	}
	if !strings.Contains(sql, "p.name ILIKE") {
		t.Fatalf("流水线必须按自身名称匹配:\n%s", sql)
	}
}

func TestQueryShape_PathColumnsAreSelected(t *testing.T) {
	repo := NewSearchRepository(openDryRun(t))

	// Path 是搜索结果展示的硬需求（§4.1「结果行必须显示所属路径」），
	// 断言 SELECT 真的带上了拼路径所需的列 —— 少选一列，运行期只会得到空路径。
	cases := []struct {
		build func() *gorm.DB
		want  []string
	}{
		{func() *gorm.DB { return repo.servicesQuery("web", 5) }, []string{"o.name AS org_name"}},
		{func() *gorm.DB { return repo.componentsQuery("web", 5) }, []string{"o.name AS org_name", "s.name AS service_name"}},
		{func() *gorm.DB { return repo.pipelinesQuery("web", 5) }, []string{"o.name AS org_name", "s.name AS service_name", "c.name AS component_name"}},
	}
	for i, tc := range cases {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			sql, _ := builtSQL(t, tc.build())
			for _, want := range tc.want {
				if !strings.Contains(sql, want) {
					t.Fatalf("SELECT 缺少拼路径必需的 %q:\n%s", want, sql)
				}
			}
		})
	}
}
