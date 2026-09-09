package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/rouroumaibing/software-distribution-platform-hub/internal/common"
	"github.com/rouroumaibing/software-distribution-platform-hub/internal/org/models"
	"github.com/rouroumaibing/software-distribution-platform-hub/internal/org/service"
)

type OrgHandler struct{ svc *service.OrgService }

func NewOrgHandler(svc *service.OrgService) *OrgHandler { return &OrgHandler{svc: svc} }

func (h *OrgHandler) RegisterRoutes(rg *gin.RouterGroup) {
	common.RegisterCRUD[models.Org](rg, "/orgs", h.svc)
	rg.GET("/orgs", h.List)
	rg.GET("/orgs/:id/service-tree", h.GetServiceTree)
}

func (h *OrgHandler) List(c *gin.Context) {
	p := common.ParsePagination(c)
	items, total, err := h.svc.List(p)
	if err != nil {
		common.Fail(c, http.StatusInternalServerError, err)
		return
	}
	common.OKPaged(c, items, total, p)
}

func (h *OrgHandler) GetServiceTree(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		common.Fail(c, http.StatusBadRequest, err)
		return
	}
	st, err := h.svc.GetServiceTree(id)
	if err != nil {
		common.Fail(c, http.StatusNotFound, err)
		return
	}
	common.OK(c, st)
}
