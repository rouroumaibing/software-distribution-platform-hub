package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/rouroumaibing/software-distribution-platform-hub/internal/catalog/models"
	"github.com/rouroumaibing/software-distribution-platform-hub/internal/catalog/service"
	"github.com/rouroumaibing/software-distribution-platform-hub/internal/common"
)

type ServiceHandler struct{ svc *service.ServiceService }

func NewServiceHandler(svc *service.ServiceService) *ServiceHandler { return &ServiceHandler{svc: svc} }

func (h *ServiceHandler) RegisterRoutes(rg *gin.RouterGroup) {
	common.RegisterCRUD[models.Service](rg, "/services", h.svc)
	rg.GET("/service-trees/:id/services", h.ListByServiceTree)
	// N-9 的按组织列服务：路径挂在 /orgs 下（而不是 /service-trees），因为调用方
	// （服务树页懒加载）手上只有组织 id，不该被迫先查一次树 id。与 orgHandler 的
	// `/orgs/:id`、`/orgs/:id/service-tree` 无路由冲突（gin 允许同参数位下挂子路径）。
	rg.GET("/orgs/:id/services", h.ListByOrg)
}

func (h *ServiceHandler) ListByServiceTree(c *gin.Context) {
	treeID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		common.Fail(c, http.StatusBadRequest, err)
		return
	}
	p := common.ParsePagination(c)
	items, total, err := h.svc.ListByServiceTree(treeID, p)
	if err != nil {
		common.Fail(c, http.StatusInternalServerError, err)
		return
	}
	common.OKPaged(c, items, total, p)
}

// ListByOrg 处理 `GET /orgs/:id/services`（附 A N-9）：返回该组织下的**直接子层**
// 服务，供服务树页"展开组织才请求服务"的懒加载使用。
func (h *ServiceHandler) ListByOrg(c *gin.Context) {
	orgID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		common.Fail(c, http.StatusBadRequest, err)
		return
	}
	p := common.ParsePagination(c)
	items, total, err := h.svc.ListByOrg(orgID, p)
	if err != nil {
		common.Fail(c, http.StatusInternalServerError, err)
		return
	}
	common.OKPaged(c, items, total, p)
}
