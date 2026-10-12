package handler

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/rouroumaibing/software-distribution-platform-hub/internal/common"
	"github.com/rouroumaibing/software-distribution-platform-hub/internal/component/service"
	"github.com/rouroumaibing/software-distribution-platform-hub/internal/middleware"
)

var (
	errVersionInvalid  = errors.New("version 必须是正整数")
	errCommentRequired = errors.New("comment 必填：回滚原因写入审计（ui-12 §4.4）")
)

// ConfigVersionHandler serves the whole-environment config version API
// (ui-12 §4.1/§4.2, N-18/N-19): list / get / diff / rollback. Routes are
// registered together with the KV config CRUD (same component scope).
type ConfigVersionHandler struct {
	svc *service.ConfigSnapshotService
}

func NewConfigVersionHandler(svc *service.ConfigSnapshotService) *ConfigVersionHandler {
	return &ConfigVersionHandler{svc: svc}
}

// RegisterRoutes mounts under /api/v1. "diff" is a static sibling of
// ":version" — same pattern the pipeline version routes already use.
func (h *ConfigVersionHandler) RegisterRoutes(rg *gin.RouterGroup) {
	rg.GET("/components/:id/config-versions", h.List)
	rg.GET("/components/:id/config-versions/diff", h.Diff)
	rg.GET("/components/:id/config-versions/:version", h.Get)
	rg.POST("/components/:id/config-versions/:version/rollback", h.Rollback)
}

// envScope parses the optional environmentId query param: absent/empty means
// the global default scope (nil), a value must be a valid uuid.
func envScope(c *gin.Context) (*uuid.UUID, error) {
	raw := c.Query("environmentId")
	if raw == "" {
		return nil, nil
	}
	id, err := uuid.Parse(raw)
	if err != nil {
		return nil, err
	}
	return &id, nil
}

func (h *ConfigVersionHandler) List(c *gin.Context) {
	componentID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		common.Fail(c, http.StatusBadRequest, err)
		return
	}
	envID, err := envScope(c)
	if err != nil {
		common.Fail(c, http.StatusBadRequest, err)
		return
	}
	limit := 50
	if v := c.Query("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 500 {
			limit = n
		}
	}
	rows, err := h.svc.List(componentID, envID, limit)
	if err != nil {
		common.Fail(c, http.StatusInternalServerError, err)
		return
	}
	common.OK(c, rows)
}

func (h *ConfigVersionHandler) Get(c *gin.Context) {
	componentID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		common.Fail(c, http.StatusBadRequest, err)
		return
	}
	envID, err := envScope(c)
	if err != nil {
		common.Fail(c, http.StatusBadRequest, err)
		return
	}
	version, err := strconv.Atoi(c.Param("version"))
	if err != nil || version <= 0 {
		common.Fail(c, http.StatusBadRequest, errVersionInvalid)
		return
	}
	row, entries, err := h.svc.Get(componentID, envID, version)
	if err != nil {
		common.Fail(c, http.StatusNotFound, err)
		return
	}
	common.OK(c, gin.H{"version": row, "entries": entries})
}

func (h *ConfigVersionHandler) Diff(c *gin.Context) {
	componentID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		common.Fail(c, http.StatusBadRequest, err)
		return
	}
	envID, err := envScope(c)
	if err != nil {
		common.Fail(c, http.StatusBadRequest, err)
		return
	}
	a, errA := strconv.Atoi(c.Query("a"))
	b, errB := strconv.Atoi(c.Query("b"))
	if errA != nil || errB != nil || a <= 0 || b <= 0 {
		common.Fail(c, http.StatusBadRequest, errVersionInvalid)
		return
	}
	d, err := h.svc.Diff(componentID, envID, a, b)
	if err != nil {
		common.Fail(c, http.StatusNotFound, err)
		return
	}
	common.OK(c, d)
}

// RollbackRequest carries the mandatory rollback reason (ui-12 §4.4) plus the
// optional change ticket; both land on the audit row via SetAuditReason.
type RollbackRequest struct {
	Comment string `json:"comment"`
	Ticket  string `json:"ticket,omitempty"`
}

func (h *ConfigVersionHandler) Rollback(c *gin.Context) {
	componentID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		common.Fail(c, http.StatusBadRequest, err)
		return
	}
	envID, err := envScope(c)
	if err != nil {
		common.Fail(c, http.StatusBadRequest, err)
		return
	}
	version, err := strconv.Atoi(c.Param("version"))
	if err != nil || version <= 0 {
		common.Fail(c, http.StatusBadRequest, errVersionInvalid)
		return
	}
	var in RollbackRequest
	if err := c.ShouldBindJSON(&in); err != nil {
		common.Fail(c, http.StatusBadRequest, err)
		return
	}
	if in.Comment == "" {
		common.Fail(c, http.StatusBadRequest, errCommentRequired)
		return
	}
	middleware.SetAuditReason(c, in.Comment, in.Ticket)

	var by *string
	if sub, ok := middleware.CurrentSubject(c); ok {
		by = &sub
	}
	created, err := h.svc.Rollback(componentID, envID, version, in.Comment, by)
	if err != nil {
		common.Fail(c, http.StatusInternalServerError, err)
		return
	}
	common.OK(c, created)
}
