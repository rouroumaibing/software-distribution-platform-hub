// Package gateway implements the Hub side of the Hub-Spoke long connection:
// a WebSocket server that accepts Runner Agents, authenticates them, tracks
// which target each connection belongs to, dispatches PipelineRunSpecs down
// to the right target, and routes status / log / heartbeat messages back up
// into the hub (e.g. to keep pipeline_runs / task_runs history in sync).
package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"

	runnerapi "github.com/rouroumaibing/software-distribution-platform-runner/api/v1alpha1"

	applog "github.com/rouroumaibing/software-distribution-platform-hub/internal/common/logger"
	"github.com/rouroumaibing/software-distribution-platform-hub/internal/target/service"
)

// ErrNoRunner is returned by Dispatch when the target has no live
// connection, so the caller can surface a clear "no runner connected" error.
var ErrNoRunner = errors.New("gateway: no runner connected for target")

// StatusHandler is invoked for every status_update relayed from a Runner.
// targetID is the resolved hub-side UUID; payload is the decoded
// StatusUpdatePayload.
type StatusHandler func(ctx context.Context, targetID uuid.UUID, payload *runnerapi.StatusUpdatePayload)

// LogHandler is invoked for every log_chunk relayed from a Runner.
type LogHandler func(ctx context.Context, payload *runnerapi.LogChunkPayload)

// AgentOpStatusHandler is invoked for every agent_op_status relayed from a
// Runner (§9.5 / §9.9 op lifecycle reports).
type AgentOpStatusHandler func(ctx context.Context, targetID uuid.UUID, payload *runnerapi.AgentOpStatusPayload)

// AgentOpLogHandler is invoked for every agent_op_log relayed from a Runner.
type AgentOpLogHandler func(ctx context.Context, targetID uuid.UUID, payload *runnerapi.AgentOpLogPayload)

// ConnectHandler is invoked once a Runner has registered and been marked
// online, so the caller can redeliver any work enqueued while the target was
// offline (e.g. pending dispatch jobs).
type ConnectHandler func(ctx context.Context, targetID uuid.UUID)

// HubServer accepts Runner WebSocket connections and multiplexes dispatch /
// inbound messages across them, keyed by target UUID.
type HubServer struct {
	targetSvc    *service.TargetService
	gatewayToken string

	upgrader websocket.Upgrader

	mu       sync.RWMutex
	conns    map[uuid.UUID]*websocket.Conn
	statusH  StatusHandler
	logH     LogHandler
	connectH ConnectHandler
	// agent op lifecycle reports (§9.5 / §9.9, UNIMPLEMENTED-MODULES-PLAN §16.5)
	opStatusH AgentOpStatusHandler
	opLogH    AgentOpLogHandler
}

// New constructs a HubServer. gatewayToken, when non-empty, must match the
// Runner's bearer token; an empty token accepts any connection (dev only).
func New(targetSvc *service.TargetService, gatewayToken string) *HubServer {
	return &HubServer{
		targetSvc:    targetSvc,
		gatewayToken: gatewayToken,
		upgrader: websocket.Upgrader{
			// Runners dial from inside managed clusters; restricting origin
			// is moot for a non-browser WS client, so we accept all. Tighten
			// with mTLS / network policy in production.
			CheckOrigin: func(r *http.Request) bool { return true },
		},
		conns: make(map[uuid.UUID]*websocket.Conn),
	}
}

// SetStatusHandler registers the callback for Runner status updates.
func (h *HubServer) SetStatusHandler(fn StatusHandler) { h.statusH = fn }

// SetLogHandler registers the callback for Runner log chunks.
func (h *HubServer) SetLogHandler(fn LogHandler) { h.logH = fn }

// SetConnectHandler registers the callback invoked when a Runner connects.
func (h *HubServer) SetConnectHandler(fn ConnectHandler) { h.connectH = fn }

// SetAgentOpStatusHandler registers the callback for Runner op status reports.
func (h *HubServer) SetAgentOpStatusHandler(fn AgentOpStatusHandler) { h.opStatusH = fn }

// SetAgentOpLogHandler registers the callback for Runner op log chunks.
func (h *HubServer) SetAgentOpLogHandler(fn AgentOpLogHandler) { h.opLogH = fn }

// ServeWS is the gin handler mounted at the gateway path. It authenticates
// the Runner, registers the connection, marks the target online, then reads
// messages until the connection drops.
func (h *HubServer) ServeWS(c *gin.Context) {
	token := extractBearer(c.GetHeader("Authorization"))
	if h.gatewayToken != "" && token != h.gatewayToken {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid gateway token"})
		return
	}
	targetName := c.GetHeader("X-Target-Name")
	if targetName == "" {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "missing X-Target-Name"})
		return
	}
	tg, err := h.targetSvc.GetByName(targetName)
	if err != nil {
		c.AbortWithStatusJSON(http.StatusNotFound, gin.H{"error": "unknown target"})
		return
	}

	ws, err := h.upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		applog.Infof("gateway: upgrade failed for target %s: %v", targetName, err)
		return
	}

	h.register(tg.ID, ws)
	if h.connectH != nil {
		h.connectH(c.Request.Context(), tg.ID)
	}
	defer h.unregister(tg.ID, ws)

	if err := h.targetSvc.Heartbeat(tg.ID, true); err != nil {
		applog.Infof("gateway: mark online failed for target %s: %v", targetName, err)
	}

	applog.Infof("gateway: target %s connected", targetName)
	h.readLoop(c.Request.Context(), tg.ID, ws)
	applog.Infof("gateway: target %s disconnected", targetName)
}

func (h *HubServer) register(targetID uuid.UUID, ws *websocket.Conn) {
	h.mu.Lock()
	h.conns[targetID] = ws
	h.mu.Unlock()
}

// unregister 移除连接。同一目标的新旧连接在滚动重启时会短暂并存：
// 旧连接的 readLoop 退出时绝不能把新连接的注册项一并删掉，否则会出现
// "心跳还在收、但 Dispatch 查不到连接"（或把派发写进已死的旧连接）。
// 因此这里只在自己仍是注册项时才删除。
func (h *HubServer) unregister(targetID uuid.UUID, ws *websocket.Conn) {
	h.mu.Lock()
	owned := h.conns[targetID] == ws
	if owned {
		delete(h.conns, targetID)
	}
	h.mu.Unlock()
	if owned {
		_ = h.targetSvc.Heartbeat(targetID, false)
	}
}

// Dispatch sends an ApplyPipelineRunPayload to the Runner managing targetID.
// It returns ErrNoRunner if that target has no live connection.
func (h *HubServer) Dispatch(ctx context.Context, targetID uuid.UUID, payload *runnerapi.ApplyPipelineRunPayload) error {
	h.mu.RLock()
	ws, ok := h.conns[targetID]
	h.mu.RUnlock()
	if !ok {
		return ErrNoRunner
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	msg := runnerapi.Message{Type: runnerapi.MessageApplyPipelineRun, Payload: raw}
	return ws.WriteJSON(msg)
}

// Approve sends an ApproveTaskPayload to the Runner managing targetID,
// relaying an approver's decision for a paused Approval task. It returns
// ErrNoRunner if that target has no live connection, so the caller can
// surface a clear "target offline, cannot deliver decision" error.
func (h *HubServer) Approve(ctx context.Context, targetID uuid.UUID, payload *runnerapi.ApproveTaskPayload) error {
	h.mu.RLock()
	ws, ok := h.conns[targetID]
	h.mu.RUnlock()
	if !ok {
		return ErrNoRunner
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	msg := runnerapi.Message{Type: runnerapi.MessageApproveTask, Payload: raw}
	return ws.WriteJSON(msg)
}

// RolloutControl sends a RolloutControlPayload to the Runner managing
// targetID, relaying an operator's pause/promote/rollback command for a
// Release task's Rollout. It returns ErrNoRunner if that target has no live
// connection, so the caller can surface a clear "target offline" error.
func (h *HubServer) RolloutControl(ctx context.Context, targetID uuid.UUID, payload *runnerapi.RolloutControlPayload) error {
	h.mu.RLock()
	ws, ok := h.conns[targetID]
	h.mu.RUnlock()
	if !ok {
		return ErrNoRunner
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	msg := runnerapi.Message{Type: runnerapi.MessageRolloutControl, Payload: raw}
	return ws.WriteJSON(msg)
}

// RerunTask sends a RerunTaskPayload to the Runner managing targetID, asking
// it to re-run a single task (and, on the Runner, every downstream task that
// depends on it) without re-dispatching the whole run. It returns ErrNoRunner
// if that target has no live connection, so the caller can surface a clear
// "target offline, retry on reconnect" error.
func (h *HubServer) RerunTask(ctx context.Context, targetID uuid.UUID, payload *runnerapi.RerunTaskPayload) error {
	h.mu.RLock()
	ws, ok := h.conns[targetID]
	h.mu.RUnlock()
	if !ok {
		return ErrNoRunner
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	msg := runnerapi.Message{Type: runnerapi.MessageRerunTask, Payload: raw}
	return ws.WriteJSON(msg)
}

// CancelPipelineRun sends a CancelPipelineRunPayload to the Runner managing
// targetID, asking it to stop the run: the Runner marks the PipelineRun
// Cancelled and tears down its in-flight TaskRuns. It returns ErrNoRunner if
// that target has no live connection.
func (h *HubServer) CancelPipelineRun(ctx context.Context, targetID uuid.UUID, payload *runnerapi.CancelPipelineRunPayload) error {
	h.mu.RLock()
	ws, ok := h.conns[targetID]
	h.mu.RUnlock()
	if !ok {
		return ErrNoRunner
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	msg := runnerapi.Message{Type: runnerapi.MessageCancelPipelineRun, Payload: raw}
	return ws.WriteJSON(msg)
}

// DispatchAgentOp sends an AgentOpDispatchPayload to the Runner managing
// targetID (§9.5 exec / §9.9 接入编排). It returns ErrNoRunner if that target
// has no live connection — the caller treats that as "op stays queued".
func (h *HubServer) DispatchAgentOp(ctx context.Context, targetID uuid.UUID, payload *runnerapi.AgentOpDispatchPayload) error {
	h.mu.RLock()
	ws, ok := h.conns[targetID]
	h.mu.RUnlock()
	if !ok {
		return ErrNoRunner
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	msg := runnerapi.Message{Type: runnerapi.MessageAgentOp, Payload: raw}
	return ws.WriteJSON(msg)
}

func (h *HubServer) readLoop(ctx context.Context, targetID uuid.UUID, ws *websocket.Conn) {
	for {
		_, data, err := ws.ReadMessage()
		if err != nil {
			// Normal on clean disconnect; log at debug level in production.
			applog.Infof("gateway: target %s read error: %v", targetID, err)
			return
		}
		var msg runnerapi.Message
		if err := json.Unmarshal(data, &msg); err != nil {
			applog.Infof("gateway: target %s bad frame: %v", targetID, err)
			continue
		}
		switch msg.Type {
		case runnerapi.MessageStatusUpdate:
			var p runnerapi.StatusUpdatePayload
			if err := json.Unmarshal(msg.Payload, &p); err != nil {
				applog.Infof("gateway: status payload decode failed: %v", err)
				continue
			}
			if h.statusH != nil {
				h.statusH(ctx, targetID, &p)
			}
		case runnerapi.MessageLogChunk:
			var p runnerapi.LogChunkPayload
			if err := json.Unmarshal(msg.Payload, &p); err != nil {
				applog.Infof("gateway: log payload decode failed: %v", err)
				continue
			}
			if h.logH != nil {
				h.logH(ctx, &p)
			}
		case runnerapi.MessageHeartbeat:
			_ = h.targetSvc.Heartbeat(targetID, true)
		case runnerapi.MessageAgentOpStatus:
			var p runnerapi.AgentOpStatusPayload
			if err := json.Unmarshal(msg.Payload, &p); err != nil {
				applog.Infof("gateway: agent op status decode failed: %v", err)
				continue
			}
			if h.opStatusH != nil {
				h.opStatusH(ctx, targetID, &p)
			}
		case runnerapi.MessageAgentOpLog:
			var p runnerapi.AgentOpLogPayload
			if err := json.Unmarshal(msg.Payload, &p); err != nil {
				applog.Infof("gateway: agent op log decode failed: %v", err)
				continue
			}
			if h.opLogH != nil {
				h.opLogH(ctx, targetID, &p)
			}
		default:
			applog.Infof("gateway: target %s unhandled message type %q", targetID, msg.Type)
		}
	}
}

func extractBearer(header string) string {
	if header == "" {
		return ""
	}
	if strings.HasPrefix(header, "Bearer ") {
		return header[len("Bearer "):]
	}
	return header
}
