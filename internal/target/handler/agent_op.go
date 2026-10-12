package handler

// AgentOpHandler exposes the agent-op ledger to the console (§9.5 / §9.9):
//   - GET  /agent-ops/:id            poll one op (weak-network fallback)
//   - GET  /targets/:id/agent-ops    ledger listing per target
//   - GET  /agent-ops/:id/stream     SSE: replay persisted logs + push live
//     status/log events, close after the op reaches a terminal state
//
// SSE shape (UNIMPLEMENTED-MODULES-PLAN §16.5): `event:` is the OpEvent kind
// (status|log), `data:` is its JSON. On connect the handler first replays the
// persisted log history (seq-ordered) so a late subscriber sees the full
// output, then streams live events from the OpStream fan-out. The stream ends
// when a terminal status event is observed — either from the live channel or
// because the op was already terminal at connect time.

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/rouroumaibing/software-distribution-platform-hub/internal/common"
	"github.com/rouroumaibing/software-distribution-platform-hub/internal/middleware"
	"github.com/rouroumaibing/software-distribution-platform-hub/internal/target/models"
	"github.com/rouroumaibing/software-distribution-platform-hub/internal/target/service"
)

// errStreamingUnsupported is returned when the response writer cannot flush
// (a gin writer always can; kept for safety, mirrors net/http's semantics).
var errStreamingUnsupported = errors.New("streaming unsupported by response writer")

type AgentOpHandler struct {
	svc *service.AgentOpService
}

func NewAgentOpHandler(svc *service.AgentOpService) *AgentOpHandler { return &AgentOpHandler{svc: svc} }

func (h *AgentOpHandler) RegisterRoutes(rg *gin.RouterGroup) {
	rg.GET("/agent-ops/:id", h.Get)
	rg.GET("/agent-ops/:id/stream", h.Stream)
	rg.GET("/targets/:id/agent-ops", h.ListByTarget)
}

// RegisterApprovalRoutes mounts the human-approval surface for gated upgrade
// ops. These are platform-level management actions (any target's upgrade may
// be approved/rejected by an ops-role holder), so the caller must mount this
// group behind RequirePlatformPermission(ops:manage) — exactly like the
// platformGroup in main.go, which only installs that middleware when auth is
// on, leaving dev mode (auth==nil) unauthenticated.
func (h *AgentOpHandler) RegisterApprovalRoutes(rg *gin.RouterGroup) {
	rg.GET("/agent-ops", h.ListAll) // 平台级台账，可选 ?status= 过滤（审批中心用 pending_approval）
	rg.POST("/agent-ops/:opId/approve", h.Approve)
	rg.POST("/agent-ops/:opId/reject", h.Reject)
}

// Get godoc
// @Summary Get one agent op
// @Description Returns the ledger row (id, opType, status, detail, message) — the polling fallback for environments where SSE is impractical.
// @Tags agent-ops
// @Produce json
// @Param id path string true "Agent op ID (UUID)"
// @Success 200 {object} common.Envelope
// @Failure 404 {object} common.Envelope
// @Router /agent-ops/{id} [get]
func (h *AgentOpHandler) Get(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		common.Fail(c, http.StatusBadRequest, err)
		return
	}
	op, err := h.svc.Get(id)
	if err != nil {
		common.AbortWithError(c, err)
		return
	}
	common.OK(c, op)
}

// ListByTarget godoc
// @Summary List agent ops of a target
// @Description Paged ledger listing (newest first) — the console's per-target operation history.
// @Tags agent-ops
// @Produce json
// @Param id path string true "Target ID (UUID)"
// @Success 200 {object} common.Envelope
// @Router /targets/{id}/agent-ops [get]
func (h *AgentOpHandler) ListByTarget(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		common.Fail(c, http.StatusBadRequest, err)
		return
	}
	p := common.ParsePagination(c)
	items, total, err := h.svc.ListByTarget(id, p)
	if err != nil {
		common.Fail(c, http.StatusInternalServerError, err)
		return
	}
	common.OKPaged(c, items, total, p)
}

// Stream godoc
// @Summary Stream one agent op's output and status (SSE)
// @Description Replays persisted log chunks, then pushes live status/log events; closes after a terminal status (succeeded|failed).
// @Tags agent-ops
// @Produce text/event-stream
// @Param id path string true "Agent op ID (UUID)"
// @Success 200 {string} string "text/event-stream"
// @Failure 404 {object} common.Envelope
// @Router /agent-ops/{id}/stream [get]
func (h *AgentOpHandler) Stream(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		common.Fail(c, http.StatusBadRequest, err)
		return
	}
	op, err := h.svc.Get(id)
	if err != nil {
		common.AbortWithError(c, err)
		return
	}

	flusher, ok := c.Writer.(http.Flusher)
	if !ok {
		common.Fail(c, http.StatusInternalServerError, errStreamingUnsupported)
		return
	}
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	// Op output may sit behind proxies; disable buffering at both hops.
	c.Header("X-Accel-Buffering", "no")

	writeEvent := func(event string, payload any) bool {
		raw, err := json.Marshal(payload)
		if err != nil {
			return true // drop the event, keep the stream alive
		}
		if _, err := c.Writer.WriteString("event: " + event + "\ndata: " + string(raw) + "\n\n"); err != nil {
			return false
		}
		flusher.Flush()
		return true
	}

	// Replay-first: current state, then the persisted log history. A
	// subscriber that connects mid-execution still sees every earlier chunk;
	// live duplicates are tolerable (idempotent console rendering), missing
	// history is not.
	if !writeEvent("status", service.OpEvent{Kind: service.OpEventStatus, Status: op}) {
		return
	}
	if logs, err := h.svc.Logs(id); err == nil {
		for i := range logs {
			if !writeEvent("log", service.OpEvent{Kind: service.OpEventLog, Log: &logs[i]}) {
				return
			}
		}
	}
	if models.IsTerminalAgentOpStatus(op.Status) {
		// Already finished: no live tailing needed.
		writeEvent("end", gin.H{"reason": "terminal"})
		return
	}

	events, cancel := h.svc.Stream().Subscribe(id)
	defer cancel()

	// Heartbeat keeps intermediaries from reaping an idle stream while the
	// op produces no output (e.g. a long Job image pull).
	heartbeat := time.NewTicker(15 * time.Second)
	defer heartbeat.Stop()

	clientGone := c.Request.Context().Done()
	for {
		select {
		case <-clientGone:
			return
		case <-heartbeat.C:
			if _, err := c.Writer.WriteString(": ping\n\n"); err != nil {
				return
			}
			flusher.Flush()
		case ev, ok := <-events:
			if !ok {
				return
			}
			if !writeEvent(ev.Kind, ev) {
				return
			}
			if ev.Kind == service.OpEventStatus && ev.Status != nil && models.IsTerminalAgentOpStatus(ev.Status.Status) {
				writeEvent("end", gin.H{"reason": "terminal"})
				return
			}
		}
	}
}

// ListAll godoc
// @Summary List agent ops across all targets (platform scope)
// @Description Platform-level ledger listing; optional `status` filter (e.g. pending_approval for the approval center). Newest first, paged.
// @Tags agent-ops
// @Produce json
// @Param status query string false "lifecycle status filter"
// @Success 200 {object} common.Envelope
// @Router /agent-ops [get]
func (h *AgentOpHandler) ListAll(c *gin.Context) {
	p := common.ParsePagination(c)
	status := c.Query("status")
	all, err := h.svc.ListByStatus(status)
	if err != nil {
		common.Fail(c, http.StatusInternalServerError, err)
		return
	}
	total := int64(len(all))
	start := (p.Page - 1) * p.PageSize
	if start > len(all) {
		start = len(all)
	}
	end := start + p.PageSize
	if end > len(all) {
		end = len(all)
	}
	common.OKPaged(c, all[start:end], total, p)
}

// Approve godoc
// @Summary Approve a gated upgrade op (platform ops role)
// @Description Releases a pending_approval upgrade op → queued → dispatched to the live Runner. Requires ops:manage.
// @Tags agent-ops
// @Produce json
// @Param opId path string true "Agent op ID (UUID)"
// @Success 200 {object} common.Envelope
// @Failure 404 {object} common.Envelope
// @Failure 409 {object} common.Envelope
// @Router /agent-ops/{opId}/approve [post]
func (h *AgentOpHandler) Approve(c *gin.Context) {
	id, err := uuid.Parse(c.Param("opId"))
	if err != nil {
		common.Fail(c, http.StatusBadRequest, err)
		return
	}
	approver, _ := middleware.CurrentSubject(c)
	if err := h.svc.ApproveUpgrade(id, approver); err != nil {
		common.AbortWithError(c, err)
		return
	}
	common.OK(c, gin.H{"id": id.String(), "status": models.AgentOpQueued})
}

// Reject godoc
// @Summary Reject a gated upgrade op (platform ops role)
// @Description Closes a pending_approval upgrade op without dispatching it. Optional JSON body {reason}. Requires ops:manage.
// @Tags agent-ops
// @Produce json
// @Param opId path string true "Agent op ID (UUID)"
// @Success 200 {object} common.Envelope
// @Failure 404 {object} common.Envelope
// @Failure 409 {object} common.Envelope
// @Router /agent-ops/{opId}/reject [post]
func (h *AgentOpHandler) Reject(c *gin.Context) {
	id, err := uuid.Parse(c.Param("opId"))
	if err != nil {
		common.Fail(c, http.StatusBadRequest, err)
		return
	}
	var body struct {
		Reason string `json:"reason"`
	}
	_ = c.ShouldBindJSON(&body)
	rejector, _ := middleware.CurrentSubject(c)
	if err := h.svc.RejectUpgrade(id, body.Reason, rejector); err != nil {
		common.AbortWithError(c, err)
		return
	}
	common.OK(c, gin.H{"id": id.String(), "status": models.AgentOpFailed})
}
