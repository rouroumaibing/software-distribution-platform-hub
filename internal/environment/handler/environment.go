package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/rouroumaibing/software-distribution-platform-hub/internal/common"
	"github.com/rouroumaibing/software-distribution-platform-hub/internal/environment/models"
	"github.com/rouroumaibing/software-distribution-platform-hub/internal/environment/service"
)

type EnvironmentHandler struct{ svc *service.EnvironmentService }

func NewEnvironmentHandler(svc *service.EnvironmentService) *EnvironmentHandler {
	return &EnvironmentHandler{svc: svc}
}

func (h *EnvironmentHandler) RegisterRoutes(rg *gin.RouterGroup) {
	common.RegisterCRUD[models.Environment](rg, "/environments", h.svc)
	rg.GET("/components/:id/environments", h.ListByComponent)
	// Connection test (§7.12.5): per-dimension checklist, persisted on the env.
	rg.POST("/environments/:id/test", h.Test)
}

func (h *EnvironmentHandler) ListByComponent(c *gin.Context) {
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

func (h *EnvironmentHandler) Test(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		common.Fail(c, http.StatusBadRequest, err)
		return
	}
	report, err := h.svc.Test(id)
	if err != nil {
		common.AbortWithError(c, err)
		return
	}
	common.OK(c, report)
}
