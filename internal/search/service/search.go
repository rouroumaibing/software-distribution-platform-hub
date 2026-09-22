package service

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/rouroumaibing/software-distribution-platform-hub/internal/common"
	"github.com/rouroumaibing/software-distribution-platform-hub/internal/search/models"
)

// 默认/上限条数。§5.3 的浮层只展示 20 条，所以默认值就是 20；上限给到 50
// （2.5 倍余量）是为了让「服务树页搜索」一次能拿到够铺满面板的结果，同时仍然
// 钉死最坏情况的响应体积 —— 这个端点是**无分页**的，没有上限就是一次全表扫描。
const (
	DefaultLimit = 20
	MaxLimit     = 50
)

// 两个查询参数错误的**模板**（ErrorCode 不同，便于客户端按码分支）。
//
// 为什么不直接 `common.ErrBadRequest.WithError(err)` 那样就地把消息写进包级单例：
// `common.ErrBadRequest` 是共享变量，WithError/WithMessage 会**改写它本身**，
// 两个并发请求会互相覆盖消息（且先到的那个消息会永久留在单例上）。这里保留模板
// 只读，由下面的构造函数**值拷贝**后写消息，杜绝共享可变状态。
var (
	errInvalidTypeTemplate  = common.NewAPIError(common.KindBase, http.StatusBadRequest, 2, "invalid search type")
	errInvalidLimitTemplate = common.NewAPIError(common.KindBase, http.StatusBadRequest, 3, "invalid search limit")
)

// ErrInvalidTypeCode / ErrInvalidLimitCode 供 handler 与测试按码断言，不必依赖消息文案。
const (
	ErrInvalidTypeCode  = "ERR.01400002"
	ErrInvalidLimitCode = "ERR.01400003"
)

func invalidTypeError(value string) *common.APIError {
	ae := *errInvalidTypeTemplate
	ae.Message = "invalid search type: " + value + " (allowed: " + strings.Join(models.AllTypes, ",") + ")"
	return &ae
}

func invalidLimitError(value string) *common.APIError {
	ae := *errInvalidLimitTemplate
	ae.Message = "invalid search limit: " + value + " (max " + strconv.Itoa(MaxLimit) + ")"
	return &ae
}

// Store is SearchService's persistence surface. An interface (same pattern as
// StageStore / ComponentStore / ServiceStore) so the parsing, filtering and
// clamping branches are unit-testable without Postgres; *repository.SearchRepository
// satisfies it as-is.
type Store interface {
	SearchServices(q string, limit int) ([]models.Hit, error)
	SearchComponents(q string, limit int) ([]models.Hit, error)
	SearchPipelines(q string, limit int) ([]models.Hit, error)
}

type SearchService struct{ store Store }

func NewSearchService(store Store) *SearchService { return &SearchService{store: store} }

// ParseTypes turns the raw `?type=` value into a validated type list.
//
// 约定（附 A N-8）：
//   - 省略 / 空串 / 纯逗号空白 → 三类全搜（调用方不必知道默认域是什么）；
//   - 出现未知取值 → **报错而不是静默忽略**。静默忽略会让 `?type=compnent`（拼错）
//     返回"三类全搜"的结果，调用方以为筛选生效了，实际上只是碰巧看到同类资源 ——
//     这类错误一旦静默就无法在客户端被发现；
//   - 去重但**保留调用方给的顺序**，因为响应按该顺序分组，顺序即 UI 分组顺序。
func ParseTypes(raw string) ([]string, error) {
	seen := map[string]bool{}
	types := make([]string, 0, len(models.AllTypes))
	for _, part := range strings.Split(raw, ",") {
		t := strings.ToLower(strings.TrimSpace(part))
		if t == "" {
			continue
		}
		if !models.IsValidType(t) {
			return nil, invalidTypeError(t)
		}
		if seen[t] {
			continue
		}
		seen[t] = true
		types = append(types, t)
	}
	if len(types) == 0 {
		types = append(types, models.AllTypes...)
	}
	return types, nil
}

// ParseLimit clamps `?limit=`. 空/0/负数/非数字一律回落 DefaultLimit（搜索框
// 边打边查，调用方漏传参数是常态，不该因此 400）；只对**明确写过头的上限**报错，
// 避免一个手滑的 `?limit=100000` 变成一次无界扫描。
func ParseLimit(raw string) (int, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return DefaultLimit, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return 0, invalidLimitError(raw)
	}
	if n <= 0 {
		return DefaultLimit, nil
	}
	if n > MaxLimit {
		return 0, invalidLimitError(raw)
	}
	return n, nil
}

// Search runs the requested per-type queries and concatenates them in the order
// the caller asked for.
//
// 关键约定（已写进 hub/API-REFERENCE.md 与 CONSOLE-UI-DESIGN.md 附 A N-8）：
//   - **q 为空返回空列表，不返回全量。** ⌘K 浮层"打开即可浏览尾部视图"是**客户端**
//     行为（用已加载索引的本地切片），服务端不该为了这个效果做一次全表扫描；
//   - `limit` 是**每类各取**条数，不是总数：响应最多 len(types) × limit 条。
//     这样"某类资源特别多"不会把另一类挤没（服务树页要按类型分组渲染，分组被
//     截断比总数大更难解释）。跨类型再排序/截断交给调用方按自己的相关性规则做
//     —— 前端 utils/search.ts 已有分档打分，后端再实现一套只会两处打架；
//   - 某类查询失败即整体失败（不吞错）：半个结果集比一次可重试的错误更危险。
func (s *SearchService) Search(q string, types []string, limit int) ([]models.Hit, error) {
	q = strings.TrimSpace(q)
	if q == "" {
		return []models.Hit{}, nil
	}
	if len(types) == 0 {
		types = models.AllTypes
	}
	if limit <= 0 {
		limit = DefaultLimit
	}
	if limit > MaxLimit {
		limit = MaxLimit
	}

	hits := make([]models.Hit, 0, len(types)*limit)
	for _, t := range types {
		var (
			batch []models.Hit
			err   error
		)
		switch t {
		case models.TypeService:
			batch, err = s.store.SearchServices(q, limit)
		case models.TypeComponent:
			batch, err = s.store.SearchComponents(q, limit)
		case models.TypePipeline:
			batch, err = s.store.SearchPipelines(q, limit)
		default:
			// ParseTypes 已挡过一遍；此处兜底防止调用方绕过解析直接传值。
			return nil, invalidTypeError(t)
		}
		if err != nil {
			return nil, err
		}
		hits = append(hits, batch...)
	}
	return hits, nil
}
