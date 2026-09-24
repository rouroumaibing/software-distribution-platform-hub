package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	runnerapi "github.com/rouroumaibing/software-distribution-platform-runner/api/v1alpha1"

	"github.com/rouroumaibing/software-distribution-platform-hub/internal/common"
	"github.com/rouroumaibing/software-distribution-platform-hub/internal/middleware"
	"github.com/rouroumaibing/software-distribution-platform-hub/internal/run/models"
	"github.com/rouroumaibing/software-distribution-platform-hub/internal/run/service"
)

type PipelineRunHandler struct{ svc *service.PipelineRunService }

func NewPipelineRunHandler(svc *service.PipelineRunService) *PipelineRunHandler {
	return &PipelineRunHandler{svc: svc}
}

// Run routes are registered individually in cmd/hub/main.go (the component-scoped
// block) rather than through a single RegisterRoutes: each one needs its own
// RequireResourceOwnership + RequirePermission wrapper, and this handler grew
// routes (stage-progress / logs / rollout control) that the old aggregate
// RegisterRoutes never listed (C-04 — the method was dead code and an outdated
// subset, so it was removed).

// Trigger godoc
// @Summary Trigger a pipeline run
// @Description Assembles a PipelineRunSpec from the pipeline's current stages/tasks and dispatches it to a target Runner. Set targetIds to fan the same trigger out to multiple environments (one independent run per target); ignored when targetId is set.
// @Tags runs
// @Accept json
// @Produce json
// @Param pipelineId path string true "Pipeline ID (UUID)"
// @Param request body models.TriggerRequest true "Trigger request"
// @Success 201 {object} common.Envelope
// @Failure 400 {object} common.Envelope
// @Failure 500 {object} common.Envelope
// @Router /pipelines/{pipelineId}/runs [post]
func (h *PipelineRunHandler) Trigger(c *gin.Context) {
	pipelineID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		common.Fail(c, http.StatusBadRequest, err)
		return
	}
	var req models.TriggerRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.Fail(c, http.StatusBadRequest, err)
		return
	}
	runs, err := h.svc.Trigger(pipelineID, &req)
	if err != nil {
		// AbortWithError 归一化错误：缺失/软删的 pipeline (gorm.ErrRecordNotFound)
		// 与明确的域错误都映射成正确状态码 (404 / 403 / 500)，不再一刀切 500
		// (run 触发路径未补齐项 #1)。
		common.AbortWithError(c, err)
		return
	}
	common.Created(c, runs)
}

// Get godoc
// @Summary Get a pipeline run by ID
// @Tags runs
// @Produce json
// @Param id path string true "Pipeline Run ID (UUID)"
// @Success 200 {object} common.Envelope
// @Failure 400 {object} common.Envelope
// @Failure 404 {object} common.Envelope
// @Router /runs/{id} [get]
func (h *PipelineRunHandler) Get(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		common.Fail(c, http.StatusBadRequest, err)
		return
	}
	item, err := h.svc.Get(id)
	if err != nil {
		common.Fail(c, http.StatusNotFound, err)
		return
	}
	common.OK(c, item)
}

// ListByPipeline godoc
// @Summary List pipeline runs for a pipeline
// @Tags runs
// @Produce json
// @Param pipelineId path string true "Pipeline ID (UUID)"
// @Param page query int false "page (default 1)"
// @Param pageSize query int false "page size (default 20, max 100)"
// @Success 200 {object} common.Envelope
// @Failure 400 {object} common.Envelope
// @Failure 500 {object} common.Envelope
// @Router /pipelines/{pipelineId}/runs [get]
func (h *PipelineRunHandler) ListByPipeline(c *gin.Context) {
	pipelineID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		common.Fail(c, http.StatusBadRequest, err)
		return
	}
	p := common.ParsePagination(c)
	items, total, err := h.svc.ListByPipeline(pipelineID, p)
	if err != nil {
		common.Fail(c, http.StatusInternalServerError, err)
		return
	}
	common.OKPaged(c, items, total, p)
}

// ListAll godoc
// @Summary List pipeline runs across all pipelines (Run Center)
// @Tags runs
// @Produce json
// @Param phase query string false "filter by phase, e.g. Failed / Running / Succeeded"
// @Param page query int false "page (default 1)"
// @Param pageSize query int false "page size (default 20, max 100)"
// @Success 200 {object} common.Envelope
// @Failure 500 {object} common.Envelope
// @Router /runs [get]
func (h *PipelineRunHandler) ListAll(c *gin.Context) {
	p := common.ParsePagination(c)
	// ?componentId= 是可选的第二维过滤（console 流水线列表的「最近运行」列靠它一次
	// 取回整组件的运行，再本地按 pipeline_id 分组）。非法 uuid 直接 400 而不是退化成
	// "不过滤" —— 静默返回全量运行会让调用方拿到看似成功、实则范围错误的结果。
	var componentID uuid.UUID
	if raw := c.Query("componentId"); raw != "" {
		parsed, err := uuid.Parse(raw)
		if err != nil {
			common.Fail(c, http.StatusBadRequest, err)
			return
		}
		componentID = parsed
	}
	items, total, err := h.svc.ListAll(p, c.Query("phase"), componentID)
	if err != nil {
		common.Fail(c, http.StatusInternalServerError, err)
		return
	}
	common.OKPaged(c, items, total, p)
}

// ListTasks godoc
// @Summary List tasks of a pipeline run
// @Tags runs
// @Produce json
// @Param id path string true "Pipeline Run ID (UUID)"
// @Success 200 {object} common.Envelope
// @Failure 400 {object} common.Envelope
// @Failure 500 {object} common.Envelope
// @Router /runs/{id}/tasks [get]
func (h *PipelineRunHandler) ListTasks(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		common.Fail(c, http.StatusBadRequest, err)
		return
	}
	items, err := h.svc.ListTasks(id)
	if err != nil {
		common.Fail(c, http.StatusInternalServerError, err)
		return
	}
	common.OK(c, items)
}

// Progress godoc
// @Summary Get a pipeline run's live progress (phase + per-task status)
// @Description Lightweight status endpoint for high-frequency console polling; returns the run phase and per-task status rows, not the full pipeline definition.
// @Tags runs
// @Produce json
// @Param id path string true "Pipeline Run ID (UUID)"
// @Success 200 {object} common.Envelope
// @Failure 400 {object} common.Envelope
// @Failure 404 {object} common.Envelope
// @Router /runs/{id}/progress [get]
func (h *PipelineRunHandler) Progress(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		common.Fail(c, http.StatusBadRequest, err)
		return
	}
	run, tasks, err := h.svc.Progress(id)
	if err != nil {
		common.Fail(c, http.StatusNotFound, err)
		return
	}
	common.OK(c, gin.H{
		"phase":   run.Phase,
		"message": run.Message,
		"tasks":   tasks,
	})
}

// StageProgress godoc
// @Summary Per-stage progress of a pipeline run
// @Description Aggregated stage rows (name/sequence/executionMode/status/done/total) derived on read from the run's TaskRuns; feeds console §7.6 stage cards.
// @Tags runs
// @Produce json
// @Param id path string true "Pipeline Run ID (UUID)"
// @Success 200 {object} common.Envelope
// @Failure 400 {object} common.Envelope
// @Failure 404 {object} common.Envelope
// @Router /runs/{id}/stage-progress [get]
func (h *PipelineRunHandler) StageProgress(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		common.Fail(c, http.StatusBadRequest, err)
		return
	}
	run, stages, err := h.svc.StageProgress(id)
	if err != nil {
		common.Fail(c, http.StatusNotFound, err)
		return
	}
	common.OK(c, gin.H{
		"run":    gin.H{"id": run.ID, "phase": run.Phase, "message": run.Message},
		"stages": stages,
	})
}

// Redispatch godoc
// @Summary Re-deliver a pipeline run's spec to its target
// @Description Re-enqueues a fresh dispatch job carrying the last payload for a run whose delivery is stuck (failed/dead) or unconfirmed. Succeeds even if the Runner is offline; the job is delivered on reconnect or by the sweeper. A run stuck in Failed is reset to Pending.
// @Tags runs
// @Produce json
// @Param id path string true "Pipeline Run ID (UUID)"
// @Success 200 {object} common.Envelope
// @Failure 400 {object} common.Envelope
// @Failure 404 {object} common.Envelope
// @Failure 500 {object} common.Envelope
// @Router /runs/{id}/redispatch [post]
func (h *PipelineRunHandler) Redispatch(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		common.Fail(c, http.StatusBadRequest, err)
		return
	}
	job, err := h.svc.Redispatch(c.Request.Context(), id)
	if err != nil {
		common.Fail(c, http.StatusInternalServerError, err)
		return
	}
	common.OK(c, job)
}

// GetLogs godoc
// @Summary Get a task's (or run-level) streamed logs for a pipeline run
// @Description Returns the ordered log chunks the hub persisted as a Runner streamed them over MessageLogChunk. Use the :name path param for a specific DAG task, or the /runs/:id/log variant for run-level output. Chunks are served in emission order.
// @Tags runs
// @Produce json
// @Param id path string true "Pipeline Run ID (UUID)"
// @Param name path string true "Task name; the run-level log uses the bucket __run__"
// @Param page query int false "page (default 1)"
// @Param pageSize query int false "page size (default 20, max 100)"
// @Success 200 {object} common.Envelope
// @Failure 400 {object} common.Envelope
// @Failure 404 {object} common.Envelope
// @Router /runs/{id}/tasks/{name}/log [get]
func (h *PipelineRunHandler) GetLogs(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		common.Fail(c, http.StatusBadRequest, err)
		return
	}
	// :name comes from the /tasks/:name/log route; the /runs/:id/log route
	// has no :name, so fall back to the query param or the run-level bucket.
	taskName := c.Param("name")
	if taskName == "" {
		taskName = c.Query("task")
	}
	if taskName == "" {
		taskName = models.RunLevelLogBucket
	}
	p := common.ParsePagination(c)
	items, total, err := h.svc.GetLogs(id, taskName, p)
	if err != nil {
		common.Fail(c, http.StatusInternalServerError, err)
		return
	}
	common.OKPaged(c, items, total, p)
}

// ApprovalDecisionRequest is the body of the approval gating endpoint.
type ApprovalDecisionRequest struct {
	// Approved is true to sign off, false to reject.
	Approved bool `json:"approved"`
	// Approver optionally overrides the identity recorded on the Runner;
	// defaults to the authenticated user (or the dev user in dev mode).
	Approver string `json:"approver,omitempty"`
	// Reason is free text for the audit trail (stored by the Runner).
	Reason string `json:"reason,omitempty"`
}

// Approve relays an approver's decision for a paused Approval task to the
// Runner via the gateway. Approver defaults to the current user; the body
// may override it (e.g. for service accounts or dev mode without SSO).
// Approve godoc
// @Summary Submit an approval decision for a paused Approval task
// @Description Relays an approver's decision to the Runner via the gateway. Approver defaults to the current user.
// @Tags runs
// @Accept json
// @Produce json
// @Param pipelineId path string true "Pipeline ID (UUID)"
// @Param runId path string true "Pipeline Run ID (UUID)"
// @Param taskName path string true "Task name awaiting approval"
// @Param request body ApprovalDecisionRequest true "Approval decision"
// @Success 200 {object} common.Envelope
// @Failure 400 {object} common.Envelope
// @Failure 500 {object} common.Envelope
// @Router /pipelines/{pipelineId}/runs/{runId}/tasks/{taskName}/decision [post]
func (h *PipelineRunHandler) Approve(c *gin.Context) {
	pipelineID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		common.Fail(c, http.StatusBadRequest, err)
		return
	}
	runID, err := uuid.Parse(c.Param("runId"))
	if err != nil {
		common.Fail(c, http.StatusBadRequest, err)
		return
	}
	taskName := c.Param("taskName")

	var req ApprovalDecisionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.Fail(c, http.StatusBadRequest, err)
		return
	}

	approver := req.Approver
	if approver == "" {
		if sub, ok := middleware.CurrentSubject(c); ok {
			approver = sub
		}
	}

	if err := h.svc.Approve(c.Request.Context(), pipelineID, runID, taskName, req.Approved, approver); err != nil {
		common.Fail(c, http.StatusInternalServerError, err)
		return
	}
	common.OK(c, gin.H{"message": "approval decision dispatched", "taskName": taskName, "approved": req.Approved})
}

// RolloutControlRequest is the body of the rollout control endpoint.
type RolloutControlRequest struct {
	// Action is one of pause | promote | rollback.
	Action string `json:"action"`
	// Operator optionally overrides the identity recorded on the Runner;
	// defaults to the authenticated user (or the dev user in dev mode).
	Operator string `json:"operator,omitempty"`
}

// ControlRollout relays an operator's progressive-delivery command (pause /
// promote / rollback) for a Release task's Rollout to the Runner via the
// gateway. Operator defaults to the current user. ControlRollout godoc
// @Summary Send a pause/promote/rollback command to a Release task's Rollout
// @Description Relays the operator command to the Runner via the gateway; the resulting phase change streams back via status updates.
// @Tags runs
// @Accept json
// @Produce json
// @Param id path string true "Pipeline Run ID (UUID)"
// @Param name path string true "Release task name"
// @Param request body RolloutControlRequest true "Rollout control command"
// @Success 200 {object} common.Envelope
// @Failure 400 {object} common.Envelope
// @Failure 500 {object} common.Envelope
// @Router /runs/{id}/tasks/{name}/rollout [post]
func (h *PipelineRunHandler) ControlRollout(c *gin.Context) {
	runID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		common.Fail(c, http.StatusBadRequest, err)
		return
	}
	taskName := c.Param("name")

	var req RolloutControlRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.Fail(c, http.StatusBadRequest, err)
		return
	}

	operator := req.Operator
	if operator == "" {
		if sub, ok := middleware.CurrentSubject(c); ok {
			operator = sub
		}
	}

	if err := h.svc.ControlRollout(c.Request.Context(), runID, taskName, runnerapi.RolloutAction(req.Action), operator); err != nil {
		common.Fail(c, http.StatusInternalServerError, err)
		return
	}
	common.OK(c, gin.H{"message": "rollout control dispatched", "taskName": taskName, "action": req.Action})
}

// Cancel godoc
// @Summary Cancel a running pipeline run
// @Description Asks the Runner owning the run to stop it: mark the PipelineRun Cancelled and tear down its in-flight TaskRuns. Only a Pending / Running / WaitingApproval run can be cancelled; a finished run returns 409. The new phase is recorded when the Runner streams its status update back.
// @Tags runs
// @Produce json
// @Param id path string true "Pipeline Run ID (UUID)"
// @Success 200 {object} common.Envelope
// @Failure 400 {object} common.Envelope
// @Failure 404 {object} common.Envelope
// @Failure 409 {object} common.Envelope
// @Router /runs/{id}/cancel [post]
func (h *PipelineRunHandler) Cancel(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		common.Fail(c, http.StatusBadRequest, err)
		return
	}
	operator := ""
	if sub, ok := middleware.CurrentSubject(c); ok {
		operator = sub
	}
	if err := h.svc.CancelRun(c.Request.Context(), id, operator); err != nil {
		// AbortWithError normalizes: the service's coded ErrRunTerminal passes
		// through as 409 + reasons, a missing run maps to 404.
		common.AbortWithError(c, err)
		return
	}
	common.OK(c, gin.H{"message": "cancel dispatched", "runId": id.String()})
}

// RerunTask godoc
// @Summary Re-run a single task of a pipeline run
// @Description Asks the Runner owning the run to reset one task (and, on the Runner, every downstream task that depends on it) so the DAG picks it up again — without re-dispatching the whole run.
// @Tags runs
// @Produce json
// @Param id path string true "Pipeline Run ID (UUID)"
// @Param name path string true "Task name"
// @Success 200 {object} common.Envelope
// @Failure 400 {object} common.Envelope
// @Failure 404 {object} common.Envelope
// @Failure 500 {object} common.Envelope
// @Router /runs/{id}/tasks/{name}/rerun [post]
func (h *PipelineRunHandler) RerunTask(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		common.Fail(c, http.StatusBadRequest, err)
		return
	}
	taskName := c.Param("name")
	operator := ""
	if sub, ok := middleware.CurrentSubject(c); ok {
		operator = sub
	}
	if err := h.svc.RerunTask(c.Request.Context(), id, taskName, operator); err != nil {
		common.AbortWithError(c, err)
		return
	}
	common.OK(c, gin.H{"message": "rerun dispatched", "runId": id.String(), "taskName": taskName})
}
