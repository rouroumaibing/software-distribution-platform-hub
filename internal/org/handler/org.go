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

// RegisterRoutes mounts the read + create/update surface on the versioned api
// group.
//
// 显式注册（不再用 common.RegisterCRUD）是因为 org 的**删除**要单独挂平台守卫
// ——RegisterCRUD 一次注册五个方法，无法只把 DELETE 分到另一个 group。
func (h *OrgHandler) RegisterRoutes(rg *gin.RouterGroup) {
	rg.POST("/orgs", h.Create)
	rg.GET("/orgs", h.List)
	rg.GET("/orgs/:id", h.Get)
	rg.PUT("/orgs/:id", h.Update)
	rg.GET("/orgs/:id/service-tree", h.GetServiceTree)
}

// RegisterAdminRoutes mounts org deletion on the platform-admin group. Deleting
// an organisation is a platform-level, high-blast-radius action
// (org/service/org.go Delete TODO: 平台级管理员权限校验), so it must not be
// reachable by every authenticated user once auth is on. Under auth-off dev mode
// the group carries no guard, so behaviour is unchanged there — consistent with
// §11 步骤 4 的平台守卫收口（与 componentRoleHandler.RegisterAdminRoutes 同手法）。
func (h *OrgHandler) RegisterAdminRoutes(rg *gin.RouterGroup) {
	rg.DELETE("/orgs/:id", h.Delete)
}

func (h *OrgHandler) Create(c *gin.Context) {
	var in models.Org
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

func (h *OrgHandler) Get(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		common.AbortWithError(c, common.ErrBadRequest.WithError(err))
		return
	}
	item, err := h.svc.Get(id)
	if err != nil {
		common.AbortWithError(c, err)
		return
	}
	common.OK(c, item)
}

func (h *OrgHandler) Update(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		common.AbortWithError(c, common.ErrBadRequest.WithError(err))
		return
	}
	var in models.Org
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

func (h *OrgHandler) Delete(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		common.AbortWithError(c, common.ErrBadRequest.WithError(err))
		return
	}
	if err := h.svc.Delete(id); err != nil {
		common.AbortWithError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
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
