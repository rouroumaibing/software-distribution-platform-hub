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
