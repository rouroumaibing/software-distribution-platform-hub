package handler

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/datatypes"

	"github.com/rouroumaibing/software-distribution-platform-hub/internal/common"
	"github.com/rouroumaibing/software-distribution-platform-hub/internal/middleware"
	"github.com/rouroumaibing/software-distribution-platform-hub/internal/pipeline/models"
	"github.com/rouroumaibing/software-distribution-platform-hub/internal/pipeline/service"
)

// TriggerHandler is the HTTP face of pipeline trigger configuration
// (ui-12 §4.3 / N-21). All routes are pipeline-scoped so the permission
// locator resolves the owning component; the webhook invocation itself is
// token-authed and mounted on the root engine by main (no bearer).
type TriggerHandler struct {
	svc *service.TriggerService
}

func NewTriggerHandler(svc *service.TriggerService) *TriggerHandler {
	return &TriggerHandler{svc: svc}
}

// tokenPrefix exposes only the leading 11 chars ("wh_" + 8 hex) — enough for
// the console to show which secret is configured without leaking it.
func tokenPrefix(t models.PipelineTrigger) models.PipelineTrigger {
	if len(t.Token) > 11 {
		t.Prefix = t.Token[:11]
	}
	return t
}

func (h *TriggerHandler) Create(c *gin.Context) {
	pipelineID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		common.Fail(c, http.StatusBadRequest, err)
		return
	}
	var in struct {
		Name    string          `json:"name"`
		Type    string          `json:"type"`
		Enabled *bool           `json:"enabled,omitempty"`
		Spec    json.RawMessage `json:"spec,omitempty"`
	}
	if err := c.ShouldBindJSON(&in); err != nil {
		common.Fail(c, http.StatusBadRequest, err)
		return
	}
	trig := &models.PipelineTrigger{
		Name:    in.Name,
		Type:    in.Type,
		Enabled: in.Enabled == nil || *in.Enabled,
		Spec:    datatypes.JSON(in.Spec),
	}
	createdBy := ""
	if sub, ok := middleware.CurrentSubject(c); ok {
		createdBy = sub
	}
	created, err := h.svc.Create(pipelineID, trig, createdBy)
	if err != nil {
		common.Fail(c, http.StatusBadRequest, err)
		return
	}
	// Webhook token is returned exactly once, here.
	resp := gin.H{"data": tokenPrefix(*created)}
	if created.Type == models.TriggerTypeWebhook {
		resp["token"] = created.Token
	}
	c.JSON(http.StatusCreated, resp)
}

func (h *TriggerHandler) List(c *gin.Context) {
	pipelineID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		common.Fail(c, http.StatusBadRequest, err)
		return
	}
	pg := common.ParsePagination(c)
	items, total, err := h.svc.List(pipelineID, pg)
	if err != nil {
		common.Fail(c, http.StatusInternalServerError, err)
		return
	}
	views := make([]models.PipelineTrigger, 0, len(items))
	for _, t := range items {
		views = append(views, tokenPrefix(t))
	}
	common.OKPaged(c, views, total, pg)
}

func (h *TriggerHandler) Update(c *gin.Context) {
	id, err := uuid.Parse(c.Param("triggerId"))
	if err != nil {
		common.Fail(c, http.StatusBadRequest, err)
		return
	}
	var in struct {
		Name    string          `json:"name"`
		Enabled bool            `json:"enabled"`
		Spec    json.RawMessage `json:"spec,omitempty"`
	}
	if err := c.ShouldBindJSON(&in); err != nil {
		common.Fail(c, http.StatusBadRequest, err)
		return
	}
	patch := &models.PipelineTrigger{
		Name:    in.Name,
		Enabled: in.Enabled,
		Spec:    datatypes.JSON(in.Spec),
	}
	updated, err := h.svc.Update(id, patch)
	if err != nil {
		common.Fail(c, http.StatusBadRequest, err)
		return
	}
	common.OK(c, tokenPrefix(*updated))
}

func (h *TriggerHandler) Delete(c *gin.Context) {
	id, err := uuid.Parse(c.Param("triggerId"))
	if err != nil {
		common.Fail(c, http.StatusBadRequest, err)
		return
	}
	if err := h.svc.Delete(id); err != nil {
		common.Fail(c, http.StatusInternalServerError, err)
		return
	}
	c.Status(http.StatusNoContent)
}

// Rotate regenerates the webhook token; new value shown exactly once.
func (h *TriggerHandler) Rotate(c *gin.Context) {
	id, err := uuid.Parse(c.Param("triggerId"))
	if err != nil {
		common.Fail(c, http.StatusBadRequest, err)
		return
	}
	token, err := h.svc.Rotate(id)
	if err != nil {
		common.Fail(c, http.StatusBadRequest, err)
		return
	}
	common.OK(c, gin.H{"token": token})
}

// CronPreview validates an expression and returns its next fire times —
// the console's 非法即时校验 + 未来 5 次预览.
func (h *TriggerHandler) CronPreview(c *gin.Context) {
	var in struct {
		Expr string `json:"expr"`
	}
	if err := c.ShouldBindJSON(&in); err != nil {
		common.Fail(c, http.StatusBadRequest, err)
		return
	}
	times, err := h.svc.PreviewCron(in.Expr, 5)
	if err != nil {
		common.Fail(c, http.StatusBadRequest, err)
		return
	}
	out := make([]string, 0, len(times))
	for _, t := range times {
		out = append(out, t.Format(time.RFC3339))
	}
	common.OK(c, gin.H{"next": out})
}

// WebhookInvoke is POST /webhooks/pipeline-triggers/:triggerId on the ROOT
// engine — token in the X-Webhook-Token header (or ?token=) IS the auth.
func (h *TriggerHandler) WebhookInvoke(c *gin.Context) {
	id, err := uuid.Parse(c.Param("triggerId"))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}
	token := c.GetHeader("X-Webhook-Token")
	if token == "" {
		token = c.Query("token")
	}
	trig, ok := h.svc.AuthorizeWebhook(id, token)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid token"})
		return
	}
	if !trig.Enabled {
		c.JSON(http.StatusConflict, gin.H{"error": "trigger disabled"})
		return
	}
	if err := h.svc.InvokeWebhook(trig); err != nil {
		common.Fail(c, http.StatusInternalServerError, err)
		return
	}
	common.OK(c, gin.H{"message": "triggered", "trigger": trig.Name})
}
