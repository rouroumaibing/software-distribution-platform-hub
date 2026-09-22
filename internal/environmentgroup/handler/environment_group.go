package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/rouroumaibing/software-distribution-platform-hub/internal/common"
	"github.com/rouroumaibing/software-distribution-platform-hub/internal/environmentgroup/models"
	"github.com/rouroumaibing/software-distribution-platform-hub/internal/environmentgroup/service"
)

type EnvironmentGroupHandler struct {
	svc *service.EnvironmentGroupService
}

func NewEnvironmentGroupHandler(svc *service.EnvironmentGroupService) *EnvironmentGroupHandler {
	return &EnvironmentGroupHandler{svc: svc}
}

func (h *EnvironmentGroupHandler) RegisterRoutes(rg *gin.RouterGroup) {
	rg.GET("/environment-groups", h.List)
	rg.POST("/environment-groups", h.Create)
	rg.GET("/environment-groups/:id", h.Get)
	rg.PUT("/environment-groups/:id", h.Update)
	rg.DELETE("/environment-groups/:id", h.Delete)
	rg.GET("/components/:id/environment-groups", h.ListByComponent)
}

func (h *EnvironmentGroupHandler) List(c *gin.Context) {
	p := common.ParsePagination(c)
	// groups are always scoped to a component via ListByComponent; this route is
	// a thin alias that returns nothing without a scope, kept for symmetry.
	common.OKPaged(c, []models.EnvironmentGroup{}, 0, p)
}

func (h *EnvironmentGroupHandler) ListByComponent(c *gin.Context) {
	componentID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		common.Fail(c, http.StatusBadRequest, err)
		return
	}
	p := common.ParsePagination(c)
	items, total, err := h.svc.ListByComponent(componentID, p)
	if err != nil {
		common.Fail(c, http.StatusInternalServerError, err)
		return
	}
	common.OKPaged(c, items, total, p)
}

func (h *EnvironmentGroupHandler) Get(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		common.Fail(c, http.StatusBadRequest, err)
		return
	}
	item, err := h.svc.Get(id)
	if err != nil {
		common.AbortWithError(c, err)
		return
	}
	common.OK(c, item)
}

func (h *EnvironmentGroupHandler) Create(c *gin.Context) {
	var in models.EnvironmentGroup
	if err := c.ShouldBindJSON(&in); err != nil {
		common.AbortWithError(c, common.ErrBadRequest.WithError(err))
		return
	}
	if err := h.svc.Create(&in); err != nil {
		common.AbortWithError(c, err)
		return
	}
	common.Created(c, in)
}

func (h *EnvironmentGroupHandler) Update(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		common.Fail(c, http.StatusBadRequest, err)
		return
	}
	var in models.EnvironmentGroup
	if err := c.ShouldBindJSON(&in); err != nil {
		common.AbortWithError(c, common.ErrBadRequest.WithError(err))
		return
	}
	if err := h.svc.Update(id, &in); err != nil {
		common.AbortWithError(c, err)
		return
	}
	common.OK(c, in)
}

func (h *EnvironmentGroupHandler) Delete(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		common.Fail(c, http.StatusBadRequest, err)
		return
	}
	if err := h.svc.Delete(id); err != nil {
		common.AbortWithError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}
