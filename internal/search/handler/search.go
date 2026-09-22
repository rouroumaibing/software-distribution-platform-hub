package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/rouroumaibing/software-distribution-platform-hub/internal/common"
	"github.com/rouroumaibing/software-distribution-platform-hub/internal/search/service"
)

// SearchHandler exposes GET /search — the cross-resource navigation search
// specified in CONSOLE-UI-DESIGN.md §5.3 / 附 A N-8.
type SearchHandler struct{ svc *service.SearchService }

func NewSearchHandler(svc *service.SearchService) *SearchHandler { return &SearchHandler{svc: svc} }

func (h *SearchHandler) RegisterRoutes(rg *gin.RouterGroup) {
	rg.GET("/search", h.Search)
}

// Search handles `GET /search?q=&type=&limit=`.
//
// 权限：本端点**刻意不做组件级权限过滤**。它只回"类型 + 名称 + 路径 + id"这类
// 导航信息，真正的资源访问仍由各详情端点的 RequirePermission 把关（点进去才 403）。
// 理由与前置条件：Epic C（账号权限）的 D1–D6 尚未拍板（ACCOUNT-PERMISSION-MODEL §10），
// 在"谁是权限权威 / 组织 claim 怎么接"定下来之前做半套过滤，只会给出一个
// 既漏又误的假安全感。拍板后本端点需要按 §10 的对账结论收窄。
func (h *SearchHandler) Search(c *gin.Context) {
	types, err := service.ParseTypes(c.Query("type"))
	if err != nil {
		common.Fail(c, http.StatusBadRequest, err)
		return
	}
	limit, err := service.ParseLimit(c.Query("limit"))
	if err != nil {
		common.Fail(c, http.StatusBadRequest, err)
		return
	}
	hits, err := h.svc.Search(c.Query("q"), types, limit)
	if err != nil {
		common.Fail(c, http.StatusInternalServerError, err)
		return
	}
	common.OK(c, hits)
}
