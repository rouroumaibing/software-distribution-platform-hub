package handler

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/rouroumaibing/software-distribution-platform-hub/internal/common"
	"github.com/rouroumaibing/software-distribution-platform-hub/internal/middleware"
	"github.com/rouroumaibing/software-distribution-platform-hub/internal/run/models"
	"github.com/rouroumaibing/software-distribution-platform-hub/internal/run/service"
)

// ApprovalCenterHandler serves the approval center reads (ui-12 §5.1) and
// the per-component approval-policy CRUD (决策 #4). Policy rows are
// component-scoped; the environment-override rows are config-plane only
// until run↔environment linkage lands (documented limitation).
type ApprovalCenterHandler struct {
	svc *service.PipelineRunService
}

func NewApprovalCenterHandler(svc *service.PipelineRunService) *ApprovalCenterHandler {
	return &ApprovalCenterHandler{svc: svc}
}

// List is GET /approvals?view=pending|initiated|decided — subject = 登录态.
func (h *ApprovalCenterHandler) List(c *gin.Context) {
	subject, _ := middleware.CurrentSubject(c)
	view := c.DefaultQuery("view", "pending")
	limit := 0
	if v := c.Query("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			limit = n
		}
	}
	items, err := h.svc.ApprovalCenter(service.ListCenterView{View: view, Subject: subject, Limit: limit})
	if err != nil {
		common.Fail(c, http.StatusBadRequest, err)
		return
	}
	common.OK(c, items)
}

// ListPolicies is GET /components/:id/approval-policies.
func (h *ApprovalCenterHandler) ListPolicies(c *gin.Context) {
	componentID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		common.Fail(c, http.StatusBadRequest, err)
		return
	}
	items, err := h.svc.GetApprovalPolicies(componentID)
	if err != nil {
		common.Fail(c, http.StatusInternalServerError, err)
		return
	}
	common.OK(c, items)
}

// UpsertPolicy is PUT /components/:id/approval-policies.
func (h *ApprovalCenterHandler) UpsertPolicy(c *gin.Context) {
	componentID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		common.Fail(c, http.StatusBadRequest, err)
		return
	}
	var in struct {
		EnvironmentID  *uuid.UUID      `json:"environmentId,omitempty"`
		Mode           string          `json:"mode"`
		MinApprovals   int             `json:"minApprovals,omitempty"`
		Assignees      json.RawMessage `json:"assignees,omitempty"`
		TimeoutSeconds int             `json:"timeoutSeconds,omitempty"`
	}
	if err := c.ShouldBindJSON(&in); err != nil {
		common.Fail(c, http.StatusBadRequest, err)
		return
	}
	assignees := ""
	if len(in.Assignees) > 0 {
		assignees = string(in.Assignees)
	}
	p := &models.ApprovalPolicy{
		ComponentID:    componentID,
		EnvironmentID:  in.EnvironmentID,
		Mode:           in.Mode,
		MinApprovals:   in.MinApprovals,
		Assignees:      assignees,
		TimeoutSeconds: in.TimeoutSeconds,
	}
	subject, _ := middleware.CurrentSubject(c)
	if err := h.svc.UpsertApprovalPolicy(p, subject); err != nil {
		common.Fail(c, http.StatusBadRequest, err)
		return
	}
	common.OK(c, p)
}
