package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/rouroumaibing/software-distribution-platform-hub/internal/cluster/models"
	"github.com/rouroumaibing/software-distribution-platform-hub/internal/cluster/service"
	"github.com/rouroumaibing/software-distribution-platform-hub/internal/common"
)

type ClusterHandler struct{ svc *service.ClusterService }

func NewClusterHandler(svc *service.ClusterService) *ClusterHandler { return &ClusterHandler{svc: svc} }

func (h *ClusterHandler) RegisterRoutes(rg *gin.RouterGroup) {
	common.RegisterCRUD[models.Cluster](rg, "/clusters", h.svc)
	rg.GET("/clusters", h.List)
}

func (h *ClusterHandler) List(c *gin.Context) {
	p := common.ParsePagination(c)
	items, total, err := h.svc.List(p)
	if err != nil {
		common.Fail(c, http.StatusInternalServerError, err)
		return
	}
	common.OKPaged(c, items, total, p)
}
