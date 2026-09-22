package repository

import (
	"strings"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/rouroumaibing/software-distribution-platform-hub/internal/search/models"
)

// SearchRepository answers cross-resource queries by joining straight to the
// owning hierarchy. Only this module knows the three path shapes.
//
// 为什么用 JOIN 而不是"三个模块各出一个小查询、service 再拼路径"：Path 是搜索
// 结果的**必需字段**（§4.1），只有 JOIN 能一次拿到；分散到三个模块会让每个调用方
// 都要自己再补两次查询 —— 正是 R-8 之前服务树页那条 N+1 请求瀑布。
//
// 软删过滤**一律显式写 `deleted_at IS NULL`**，不依赖 GORM 的软删自动条件：
// 这里用的是 Table()+Joins()，自动条件只作用于 Model() 指定的那张表，JOIN 进来的
// services / components / pipelines / orgs 必须自己过滤，否则会搜出已删资源的行
// （orgs / services / components / pipelines 恰好都是 Base 软删表）。
// 反向陷阱：service_trees 是 BaseNoSoftDelete（**没有** deleted_at 列），对它加该
// 条件会直接 SQL 报错 —— 所以它只作 JOIN 的桥，不加过滤。
//
// 三个 *Query 方法是**可测的查询构造器**：真正的查询逻辑（SELECT/JOIN/WHERE/ORDER）
// 全在它们里面，Scan 由后面的 Search* 方法补。这样 DB-free 测试能拿到构造好的
// *gorm.DB 直接断言生成 SQL 与绑定参数（见 search_dryrun_test.go），不必连 Postgres。
type SearchRepository struct{ db *gorm.DB }

func NewSearchRepository(db *gorm.DB) *SearchRepository { return &SearchRepository{db: db} }

// escapeLike 转义 LIKE 元字符。
//
// 为什么必须转义：不转义时用户输入里的 `_`（如 "user_api"）是单字符通配符，
// 只输入一个 `_` 更会命中全表；`%` 同理会把结果放大到无意义。反斜杠是 Postgres
// LIKE 的默认转义符，所以 `\_` 即字面下划线，无需额外 ESCAPE 子句。
func escapeLike(q string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(q)
}

// likePattern 是"包含"模式；prefixPattern 是"前缀"模式（给相关度排序用）。
func likePattern(q string) string   { return "%" + escapeLike(q) + "%" }
func prefixPattern(q string) string { return escapeLike(q) + "%" }

// relevanceOrder builds the ORDER BY that puts 精确命中 → 名称前缀 → 名称包含.
//
// 少了它，`q=svc-a` 时 `svc-a-old` 可能排到 `svc-a` 前面 —— 而搜索结果第一行
// 就是键盘回车要打开的那条（与前端 utils/search.ts 的分档打分同源）。
//
// 为什么走 clause.OrderBy{Expression:...} 而不是 db.Order(gorm.Expr(...))：
// GORM 的 Order() 只识别 string / clause.OrderBy / clause.OrderByColumn 三种入参，
// **传 gorm.Expr 会被静默忽略**（不报错、直接没有 ORDER BY）。带 `?` 占位符的
// 表达式必须用这个形状才能既绑定参数又真的生效 —— search_dryrun_test.go 里有一条
// 断言专门钉住"ORDER BY 真的出现在 SQL 里"。
func relevanceOrder(alias, q string) clause.OrderBy {
	name := alias + ".name"
	return clause.OrderBy{Expression: clause.Expr{
		SQL:  "CASE WHEN lower(" + name + ") = lower(?) THEN 0 WHEN " + name + " ILIKE ? THEN 1 ELSE 2 END, " + name + " ASC",
		Vars: []any{q, prefixPattern(q)},
	}}
}

func (r *SearchRepository) servicesQuery(q string, limit int) *gorm.DB {
	return r.db.Table("services AS s").
		Select("s.id AS id, s.name AS name, s.key AS key, o.name AS org_name").
		Joins("JOIN service_trees AS st ON st.id = s.service_tree_id").
		Joins("JOIN orgs AS o ON o.id = st.org_id").
		Where("s.deleted_at IS NULL AND o.deleted_at IS NULL").
		Where("s.name ILIKE ? OR s.key ILIKE ?", likePattern(q), likePattern(q)).
		Order(relevanceOrder("s", q)).
		Limit(limit)
}

func (r *SearchRepository) SearchServices(q string, limit int) ([]models.Hit, error) {
	type row struct {
		ID      uuid.UUID
		Name    string
		Key     string
		OrgName string
	}
	var rows []row
	if err := r.servicesQuery(q, limit).Scan(&rows).Error; err != nil {
		return nil, err
	}
	hits := make([]models.Hit, 0, len(rows))
	for _, row := range rows {
		hits = append(hits, models.Hit{
			Type:    models.TypeService,
			ID:      row.ID,
			Name:    row.Name,
			Path:    joinPath(row.OrgName),
			Keyword: row.Key,
		})
	}
	return hits, nil
}

func (r *SearchRepository) componentsQuery(q string, limit int) *gorm.DB {
	return r.db.Table("components AS c").
		Select("c.id AS id, c.name AS name, c.key AS key, o.name AS org_name, s.name AS service_name").
		Joins("JOIN services AS s ON s.id = c.service_id").
		Joins("JOIN service_trees AS st ON st.id = s.service_tree_id").
		Joins("JOIN orgs AS o ON o.id = st.org_id").
		Where("c.deleted_at IS NULL AND s.deleted_at IS NULL AND o.deleted_at IS NULL").
		Where("c.name ILIKE ? OR c.key ILIKE ?", likePattern(q), likePattern(q)).
		Order(relevanceOrder("c", q)).
		Limit(limit)
}

func (r *SearchRepository) SearchComponents(q string, limit int) ([]models.Hit, error) {
	type row struct {
		ID          uuid.UUID
		Name        string
		Key         string
		OrgName     string
		ServiceName string
	}
	var rows []row
	if err := r.componentsQuery(q, limit).Scan(&rows).Error; err != nil {
		return nil, err
	}
	hits := make([]models.Hit, 0, len(rows))
	for _, row := range rows {
		hits = append(hits, models.Hit{
			Type:    models.TypeComponent,
			ID:      row.ID,
			Name:    row.Name,
			Path:    joinPath(row.OrgName, row.ServiceName),
			Keyword: row.Key,
		})
	}
	return hits, nil
}

func (r *SearchRepository) pipelinesQuery(q string, limit int) *gorm.DB {
	return r.db.Table("pipelines AS p").
		Select("p.id AS id, p.name AS name, p.kind AS kind, o.name AS org_name, s.name AS service_name, c.name AS component_name").
		Joins("JOIN components AS c ON c.id = p.component_id").
		Joins("JOIN services AS s ON s.id = c.service_id").
		Joins("JOIN service_trees AS st ON st.id = s.service_tree_id").
		Joins("JOIN orgs AS o ON o.id = st.org_id").
		Where("p.deleted_at IS NULL AND c.deleted_at IS NULL AND s.deleted_at IS NULL AND o.deleted_at IS NULL").
		Where("p.name ILIKE ?", likePattern(q)).
		Order(relevanceOrder("p", q)).
		Limit(limit)
}

func (r *SearchRepository) SearchPipelines(q string, limit int) ([]models.Hit, error) {
	type row struct {
		ID            uuid.UUID
		Name          string
		Kind          string
		OrgName       string
		ServiceName   string
		ComponentName string
	}
	var rows []row
	if err := r.pipelinesQuery(q, limit).Scan(&rows).Error; err != nil {
		return nil, err
	}
	hits := make([]models.Hit, 0, len(rows))
	for _, row := range rows {
		hits = append(hits, models.Hit{
			Type: models.TypePipeline,
			ID:   row.ID,
			Name: row.Name,
			// 流水线**不匹配组件名，只在路径里显示它**：若让组件名进匹配，
			// 搜一个组件会连带命中它名下全部流水线，把真正的组件命中挤出前 20 条。
			Path:    joinPath(row.OrgName, row.ServiceName, row.ComponentName),
			Keyword: row.Kind,
		})
	}
	return hits, nil
}

// joinPath 拼展示路径。分隔符固定 " / "，与 console 端 useResourceMap 的
// `${org} / ${service}` 完全一致 —— 同一资源在服务树页与 ⌘K 浮层里必须显示同一条路径。
func joinPath(parts ...string) string {
	kept := make([]string, 0, len(parts))
	for _, p := range parts {
		if p != "" {
			kept = append(kept, p)
		}
	}
	return strings.Join(kept, " / ")
}
