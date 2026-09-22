package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/rouroumaibing/software-distribution-platform-hub/internal/common"
	"github.com/rouroumaibing/software-distribution-platform-hub/internal/permission/models"
	"github.com/rouroumaibing/software-distribution-platform-hub/internal/permission/service"
)

// ComponentRoleHandler exposes the §7.3 component-scoped roles
// (component_roles) so the console can populate the role picker when granting
// a ComponentRoleBinding, and so a platform admin can define custom roles.
//
// Built-in roles are seeded (component-viewer / component-editor /
// component-approver / component-admin) and are immutable through this API;
// org-scoped custom roles (org_id != NULL) are created / edited / deleted here
// (B-11 自定义角色).
type ComponentRoleHandler struct{ svc *service.ComponentRoleService }

func NewComponentRoleHandler(svc *service.ComponentRoleService) *ComponentRoleHandler {
	return &ComponentRoleHandler{svc: svc}
}

// RegisterRoutes mounts the **read** surface. It stays on the bare `api`
// group on purpose: the console's PermissionsTab uses this list to populate the
// role picker for any component-scoped user, so requiring the platform-level
// `user:manage` action here would break ordinary component admins.
func (h *ComponentRoleHandler) RegisterRoutes(rg *gin.RouterGroup) {
	rg.GET("/component-roles", h.List)
	rg.GET("/component-roles/:id", h.Get)
}

// RegisterAdminRoutes mounts the **write** surface (B-11 自定义角色).
//
// 定义角色是平台级动作 —— 与 `/platform-roles` 同级。所以它挂到
// `platformGroup`（开启鉴权后要求平台级 `user:manage`），而不是跟 List 一起裸挂：
// 角色是权限的来源，能改角色就等于能给自己加权限，写端点不能只靠"登录了"。
func (h *ComponentRoleHandler) RegisterAdminRoutes(rg *gin.RouterGroup) {
	rg.POST("/component-roles", h.Create)
	rg.PUT("/component-roles/:id", h.Update)
	rg.DELETE("/component-roles/:id", h.Delete)
}

func (h *ComponentRoleHandler) List(c *gin.Context) {
	items, err := h.svc.List()
	if err != nil {
		failPermission(c, err)
		return
	}
	common.OK(c, items)
}

func (h *ComponentRoleHandler) Get(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		common.Fail(c, http.StatusBadRequest, err)
		return
	}
	item, serr := h.svc.Get(id)
	if serr != nil {
		failPermission(c, serr)
		return
	}
	common.OK(c, item)
}

func (h *ComponentRoleHandler) Create(c *gin.Context) {
	var in models.ComponentRole
	if err := c.ShouldBindJSON(&in); err != nil {
		common.Fail(c, http.StatusBadRequest, err)
		return
	}
	if err := h.svc.Create(&in); err != nil {
		failPermission(c, err)
		return
	}
	common.Created(c, in)
}

func (h *ComponentRoleHandler) Update(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		common.Fail(c, http.StatusBadRequest, err)
		return
	}
	var in models.ComponentRole
	if err := c.ShouldBindJSON(&in); err != nil {
		common.Fail(c, http.StatusBadRequest, err)
		return
	}
	if err := h.svc.Update(id, &in); err != nil {
		failPermission(c, err)
		return
	}
	updated, gerr := h.svc.Get(id)
	if gerr != nil {
		failPermission(c, gerr)
		return
	}
	common.OK(c, updated)
}

func (h *ComponentRoleHandler) Delete(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		common.Fail(c, http.StatusBadRequest, err)
		return
	}
	if err := h.svc.Delete(id); err != nil {
		failPermission(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}
