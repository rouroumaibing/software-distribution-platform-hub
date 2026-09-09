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
