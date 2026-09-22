package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/rouroumaibing/software-distribution-platform-hub/internal/common"
	"github.com/rouroumaibing/software-distribution-platform-hub/internal/target/models"
	"github.com/rouroumaibing/software-distribution-platform-hub/internal/target/service"
)

type TargetHandler struct{ svc *service.TargetService }

func NewTargetHandler(svc *service.TargetService) *TargetHandler { return &TargetHandler{svc: svc} }

func (h *TargetHandler) RegisterRoutes(rg *gin.RouterGroup) {
	common.RegisterCRUD[models.Target](rg, "/targets", h.svc)
	rg.GET("/targets", h.List)
	// One-time bootstrap token for enrolling a Runner Agent (§9.9).
	rg.POST("/targets/:id/enroll-token", h.EnrollToken)
}

func (h *TargetHandler) List(c *gin.Context) {
	p := common.ParsePagination(c)
	items, total, err := h.svc.List(p)
	if err != nil {
		common.Fail(c, http.StatusInternalServerError, err)
		return
	}
	common.OKPaged(c, items, total, p)
}

func (h *TargetHandler) EnrollToken(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		common.Fail(c, http.StatusBadRequest, err)
		return
	}
	token, err := h.svc.GenerateEnrollToken(id)
	if err != nil {
		common.AbortWithError(c, err)
		return
	}
	common.OK(c, gin.H{"targetId": id.String(), "enrollToken": token, "expiresIn": "24h"})
}
