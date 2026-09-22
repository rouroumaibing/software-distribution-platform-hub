package handler

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/rouroumaibing/software-distribution-platform-hub/internal/common"
	"github.com/rouroumaibing/software-distribution-platform-hub/internal/middleware"
	"github.com/rouroumaibing/software-distribution-platform-hub/internal/permission/models"
	"github.com/rouroumaibing/software-distribution-platform-hub/internal/permission/service"
)

// PermissionRequestHandler exposes the approval flow (ACCOUNT-PERMISSION-MODEL
// §7). It is deliberately separate from pipeline approvals — this one grants a
// role binding, that one clears a pipeline gate.
type PermissionRequestHandler struct {
	svc *service.PermissionRequestService
}

func NewPermissionRequestHandler(svc *service.PermissionRequestService) *PermissionRequestHandler {
	return &PermissionRequestHandler{svc: svc}
}

func (h *PermissionRequestHandler) RegisterRoutes(rg *gin.RouterGroup) {
	rg.GET("/permission-requests", h.List)
	rg.POST("/permission-requests", h.Create)
	rg.POST("/permission-requests/:id/approve", h.Approve)
	rg.POST("/permission-requests/:id/reject", h.Reject)
}

func (h *PermissionRequestHandler) List(c *gin.Context) {
	items, err := h.svc.List(c.Query("status"))
	if err != nil {
		failPermission(c, err)
		return
	}
	common.OK(c, items)
}

type requestInput struct {
	RequestedRoleID       uuid.UUID  `json:"requestedRoleId"`
	RequestedRoleKind     string     `json:"requestedRoleKind"`
	RequestedResourceType string     `json:"requestedResourceType,omitempty"`
	RequestedResourceID   uuid.UUID  `json:"requestedResourceId,omitempty"`
	Reason                string     `json:"reason"`
	ExpiresAt             *time.Time `json:"expiresAt,omitempty"`
}

func (h *PermissionRequestHandler) Create(c *gin.Context) {
	var in requestInput
	if err := c.ShouldBindJSON(&in); err != nil {
		common.Fail(c, http.StatusBadRequest, err)
		return
	}
	subject, ok := middleware.CurrentSubject(c)
	if !ok || subject == "" {
		common.Fail(c, http.StatusUnauthorized, common.ErrUnauthorized)
		return
	}
	req := &models.PermissionRequest{
		Requester:             subject,
		RequestedRoleID:       in.RequestedRoleID,
		RequestedRoleKind:     in.RequestedRoleKind,
		RequestedResourceType: in.RequestedResourceType,
		RequestedResourceID:   in.RequestedResourceID,
		Reason:                in.Reason,
		ExpiresAt:             in.ExpiresAt,
	}
	if err := h.svc.Create(req); err != nil {
		failPermission(c, err)
		return
	}
	common.Created(c, req)
}

type decisionInput struct {
	EffectiveAt *time.Time `json:"effectiveAt,omitempty"`
	ExpiresAt   *time.Time `json:"expiresAt,omitempty"`
	Reason      string     `json:"reason,omitempty"`
}

func (h *PermissionRequestHandler) Approve(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		common.Fail(c, http.StatusBadRequest, err)
		return
	}
	var in decisionInput
	_ = c.ShouldBindJSON(&in)
	approver, _ := middleware.CurrentSubject(c)
	if aerr := h.svc.Approve(c.Request.Context(), id, approver, in.EffectiveAt, in.ExpiresAt); aerr != nil {
		failPermission(c, aerr)
		return
	}
	updated, gerr := h.svc.Get(id)
	if gerr != nil {
		failPermission(c, gerr)
		return
	}
	common.OK(c, updated)
}

func (h *PermissionRequestHandler) Reject(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		common.Fail(c, http.StatusBadRequest, err)
		return
	}
	var in decisionInput
	_ = c.ShouldBindJSON(&in)
	approver, _ := middleware.CurrentSubject(c)
	if rerr := h.svc.Reject(id, approver, in.Reason); rerr != nil {
		failPermission(c, rerr)
		return
	}
	updated, gerr := h.svc.Get(id)
	if gerr != nil {
		failPermission(c, gerr)
		return
	}
	common.OK(c, updated)
}
