package handler

import (
	"net/http"

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
	if in.OwnerUser == nil && in.OwnerGroup == nil {
		if uid, ok := middleware.CurrentUserID(c); ok {
			in.OwnerUser = &uid
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
		common.Fail(c, http.StatusInternalServerError, err)
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
