package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/rouroumaibing/software-distribution-platform-hub/internal/common"
	"github.com/rouroumaibing/software-distribution-platform-hub/internal/permission/service"
)

// ResourceOwnershipHandler exposes the authoritative ownership table
// (ACCOUNT-PERMISSION-MODEL §3). Keyed by (resourceType, resourceId) rather
// than a synthetic id, so the routes read as "the owner of this component".
type ResourceOwnershipHandler struct {
	svc *service.ResourceOwnershipService
}

func NewResourceOwnershipHandler(svc *service.ResourceOwnershipService) *ResourceOwnershipHandler {
	return &ResourceOwnershipHandler{svc: svc}
}

func (h *ResourceOwnershipHandler) RegisterRoutes(rg *gin.RouterGroup) {
	rg.GET("/resource-ownership", h.Get)
	rg.PUT("/resource-ownership", h.Set)
	rg.DELETE("/resource-ownership", h.Delete)
}

func (h *ResourceOwnershipHandler) Get(c *gin.Context) {
	rt := c.Query("resourceType")
	rid, err := uuid.Parse(c.Query("resourceId"))
	if err != nil || rt == "" {
		common.Fail(c, http.StatusBadRequest, common.ErrBadRequest.WithMessage("resourceType and resourceId are required"))
		return
	}
	o, gerr := h.svc.Get(rt, rid)
	if gerr != nil {
		failPermission(c, gerr)
		return
	}
	common.OK(c, o)
}

// ownershipInput is the PUT body. allowedOrgs is optional (empty = owner-only).
type ownershipInput struct {
	ResourceType string      `json:"resourceType"`
	ResourceID   uuid.UUID   `json:"resourceId"`
	OwnerOrg     uuid.UUID   `json:"ownerOrg"`
	AllowedOrgs  []uuid.UUID `json:"allowedOrgs,omitempty"`
}

func (h *ResourceOwnershipHandler) Set(c *gin.Context) {
	var in ownershipInput
	if err := c.ShouldBindJSON(&in); err != nil {
		common.Fail(c, http.StatusBadRequest, err)
		return
	}
	if in.ResourceType == "" || in.ResourceID == uuid.Nil || in.OwnerOrg == uuid.Nil {
		common.Fail(c, http.StatusBadRequest, common.ErrBadRequest.WithMessage("resourceType, resourceId and ownerOrg are required"))
		return
	}
	o, err := h.svc.Set(in.ResourceType, in.ResourceID, in.OwnerOrg, in.AllowedOrgs)
	if err != nil {
		failPermission(c, err)
		return
	}
	common.OK(c, o)
}

func (h *ResourceOwnershipHandler) Delete(c *gin.Context) {
	rt := c.Query("resourceType")
	rid, err := uuid.Parse(c.Query("resourceId"))
	if err != nil || rt == "" {
		common.Fail(c, http.StatusBadRequest, common.ErrBadRequest.WithMessage("resourceType and resourceId are required"))
		return
	}
	if derr := h.svc.Delete(rt, rid); derr != nil {
		failPermission(c, derr)
		return
	}
	c.Status(http.StatusNoContent)
}
