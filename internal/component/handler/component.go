package handler

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/rouroumaibing/software-distribution-platform-hub/internal/common"
	"github.com/rouroumaibing/software-distribution-platform-hub/internal/component/models"
	"github.com/rouroumaibing/software-distribution-platform-hub/internal/component/service"
	"github.com/rouroumaibing/software-distribution-platform-hub/internal/middleware"
)

type ComponentHandler struct{ svc *service.ComponentService }

func NewComponentHandler(svc *service.ComponentService) *ComponentHandler {
	return &ComponentHandler{svc: svc}
}

func (h *ComponentHandler) RegisterRoutes(rg *gin.RouterGroup) {
	// POST is registered manually (not via common.RegisterCRUD) so Create
	// can stamp the owner from the authenticated identity and trigger the
	// §7.4 owner→component-admin auto-binding. GET/PUT/DELETE mirror the
	// RegisterCRUD shape.
	rg.POST("/components", h.Create)
	rg.GET("/components/:id", h.Get)
	rg.PUT("/components/:id", h.Update)
	rg.DELETE("/components/:id", h.Delete)
	rg.GET("/services/:id/components", h.ListByService)
	rg.GET("/components/:id/versions", h.ListDeployableVersions)
}

// Create stamps the component owner from the authenticated user when the
// request body leaves it unset, then persists (which auto-binds the owner
// as component-admin — see service.ComponentService.Create).
func (h *ComponentHandler) Create(c *gin.Context) {
	var in models.Component
	if err := c.ShouldBindJSON(&in); err != nil {
		common.Fail(c, http.StatusBadRequest, err)
		return
	}
	// §7.4: 未显式指定 owner 时，创建者即为 owner（默认审批人/管理员）。
	// owner 记的是 token `sub`（§5.3）—— D3 之后仓库里没有本地用户 id 可取。
	if in.OwnerSub == nil && in.OwnerGroup == nil {
		if sub, ok := middleware.CurrentSubject(c); ok {
			in.OwnerSub = &sub
		}
	}
	if err := h.svc.Create(&in); err != nil {
		common.Fail(c, http.StatusInternalServerError, err)
		return
	}
	common.Created(c, in)
}

func (h *ComponentHandler) Get(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		common.Fail(c, http.StatusBadRequest, err)
		return
	}
	item, err := h.svc.Get(id)
	if err != nil {
		common.Fail(c, http.StatusNotFound, err)
		return
	}
	common.OK(c, item)
}

func (h *ComponentHandler) Update(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		common.Fail(c, http.StatusBadRequest, err)
		return
	}
	var in models.Component
	if err := c.ShouldBindJSON(&in); err != nil {
		common.Fail(c, http.StatusBadRequest, err)
		return
	}
	if err := h.svc.Update(id, &in); err != nil {
		common.Fail(c, http.StatusInternalServerError, err)
		return
	}
	common.OK(c, in)
}

func (h *ComponentHandler) Delete(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		common.Fail(c, http.StatusBadRequest, err)
		return
	}
	if err := h.svc.Delete(id); err != nil {
		// 透传 service 层结构化 *common.APIError 的状态码（如「有活跃 run」→ 409），
		// 而非一律 500（与 pipeline 删除处理器一致）。用 errors.As 以穿透 wrap。
		var ae *common.APIError
		if errors.As(err, &ae) {
			common.Fail(c, ae.Code, err)
		} else {
			common.Fail(c, http.StatusInternalServerError, err)
		}
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *ComponentHandler) ListByService(c *gin.Context) {
	serviceID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		common.Fail(c, http.StatusBadRequest, err)
		return
	}
	p := common.ParsePagination(c)
	items, total, err := h.svc.ListByService(serviceID, p)
	if err != nil {
		common.Fail(c, http.StatusInternalServerError, err)
		return
	}
	common.OKPaged(c, items, total, p)
}

// ListDeployableVersions handles GET /components/:id/versions (O-38): the set of
// released versions a canary rollback can target. Source is the artifact store.
// The list is capped by ?limit= with default 50 and upper bound 100, mirroring
// notification.List's "同型夹取" — a pure display cap, not an offset window.
//
//	@Summary	列出组件可部署版本
//	@Tags		components
//	@Produce	json
//	@Param		id		path		string	true	"组件 ID"
//	@Param		limit	query		int		false	"返回上限（默认 50，最大 100）"
//	@Success	200		{object}	common.Envelope{data=[]service.DeployableVersion}
//	@Router		/components/{id}/versions [get]
func (h *ComponentHandler) ListDeployableVersions(c *gin.Context) {
	componentID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		common.Fail(c, http.StatusBadRequest, err)
		return
	}
	limit, _ := strconv.Atoi(c.Query("limit"))
	if limit <= 0 {
		limit = 50
	}
	if limit > 100 {
		limit = 100
	}
	items, err := h.svc.ListDeployableVersions(componentID, limit)
	if err != nil {
		common.Fail(c, http.StatusInternalServerError, err)
		return
	}
	common.OK(c, items)
}
