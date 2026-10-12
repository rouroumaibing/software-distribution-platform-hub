package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/rouroumaibing/software-distribution-platform-hub/internal/common"
	applog "github.com/rouroumaibing/software-distribution-platform-hub/internal/common/logger"
)

var errStreamingUnsupported = errors.New("streaming unsupported by response writer")

// StreamLogs is GET /runs/:id/log/stream — run-level SSE (ui-12 §5.5 / N-27).
//
// 与 agent_op SSE（pub/sub fan-out）不同，运行日志没有总线：runner 的日志
// 块由状态回写落 TaskRunLog（append-only）。因此这里用**轮询式 SSE**：
// 回放全部历史 → 每 2s 拉 created_at 游标之后的增量 → run 终态后 flush
// 残量并关闭。断线由 EventSource 自动重连（回放保证不丢历史）。
func (h *PipelineRunHandler) StreamLogs(c *gin.Context) {
	runID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		common.Fail(c, http.StatusBadRequest, err)
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
	c.Header("X-Accel-Buffering", "no")

	writeEvent := func(event string, payload any) bool {
		raw, err := json.Marshal(payload)
		if err != nil {
			return true // 丢事件保活
		}
		if _, err := c.Writer.WriteString("event: " + event + "\ndata: " + string(raw) + "\n\n"); err != nil {
			return false
		}
		flusher.Flush()
		return true
	}

	sent := map[string]bool{}
	var cursor time.Time
	// 回放：历史日志一次全量（每批 500），保证中途接入的订阅者不丢上下文。
	for {
		chunks, err := h.svc.StreamLogTail(runID, cursor, 500)
		if err != nil {
			applog.Warnf("run: log tail fetch failed for %s: %v", runID, err)
			break
		}
		fresh := 0
		for i := range chunks {
			if sent[chunks[i].ID.String()] {
				continue
			}
			sent[chunks[i].ID.String()] = true
			cursor = chunks[i].CreatedAt
			fresh++
			if !writeEvent("log", &chunks[i]) {
				return
			}
		}
		if fresh == 0 {
			break
		}
	}

	// 运行终态直接收尾，不挂轮询。
	if run, err := h.svc.Get(runID); err == nil && isTerminalRunPhase(string(run.Phase)) {
		writeEvent("end", gin.H{"reason": "terminal"})
		return
	}

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	clientGone := c.Request.Context().Done()
	for {
		select {
		case <-clientGone:
			return
		case <-ticker.C:
			chunks, err := h.svc.StreamLogTail(runID, cursor, 500)
			if err != nil {
				applog.Warnf("run: log tail poll failed for %s: %v", runID, err)
				continue
			}
			for i := range chunks {
				if sent[chunks[i].ID.String()] {
					continue
				}
				sent[chunks[i].ID.String()] = true
				cursor = chunks[i].CreatedAt
				if !writeEvent("log", &chunks[i]) {
					return
				}
			}
			if run, err := h.svc.Get(runID); err == nil && isTerminalRunPhase(string(run.Phase)) {
				writeEvent("end", gin.H{"reason": "terminal"})
				return
			}
		}
	}
}

// isTerminalRunPhase mirrors service.isTerminalPhase (unexported there).
func isTerminalRunPhase(p string) bool {
	return p == "Succeeded" || p == "Failed" || p == "Cancelled"
}
