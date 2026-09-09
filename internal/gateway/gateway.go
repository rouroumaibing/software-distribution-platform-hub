// Package gateway implements the Hub side of the Hub-Spoke long connection:
// a WebSocket server that accepts Runner Agents, authenticates them, tracks
// which cluster each connection belongs to, dispatches PipelineRunSpecs down
// to the right cluster, and routes status / log / heartbeat messages back up
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

	"github.com/rouroumaibing/software-distribution-platform-hub/internal/cluster/service"
	applog "github.com/rouroumaibing/software-distribution-platform-hub/internal/common/logger"
)

// ErrNoRunner is returned by Dispatch when the target cluster has no live
// connection, so the caller can surface a clear "no runner connected" error.
var ErrNoRunner = errors.New("gateway: no runner connected for cluster")

// StatusHandler is invoked for every status_update relayed from a Runner.
// clusterID is the resolved hub-side UUID; payload is the decoded
// StatusUpdatePayload.
type StatusHandler func(ctx context.Context, clusterID uuid.UUID, payload *runnerapi.StatusUpdatePayload)

// LogHandler is invoked for every log_chunk relayed from a Runner.
type LogHandler func(ctx context.Context, payload *runnerapi.LogChunkPayload)

// ConnectHandler is invoked once a Runner has registered and been marked
// online, so the caller can redeliver any work enqueued while the cluster was
// offline (e.g. pending dispatch jobs).
type ConnectHandler func(ctx context.Context, clusterID uuid.UUID)

// HubServer accepts Runner WebSocket connections and multiplexes dispatch /
// inbound messages across them, keyed by cluster UUID.
type HubServer struct {
	clusterSvc   *service.ClusterService
	gatewayToken string

	upgrader websocket.Upgrader

	mu       sync.RWMutex
	conns    map[uuid.UUID]*websocket.Conn
	statusH  StatusHandler
	logH     LogHandler
	connectH ConnectHandler
}

// New constructs a HubServer. gatewayToken, when non-empty, must match the
// Runner's bearer token; an empty token accepts any connection (dev only).
func New(clusterSvc *service.ClusterService, gatewayToken string) *HubServer {
	return &HubServer{
		clusterSvc:   clusterSvc,
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

// ServeWS is the gin handler mounted at the gateway path. It authenticates
// the Runner, registers the connection, marks the cluster online, then reads
// messages until the connection drops.
func (h *HubServer) ServeWS(c *gin.Context) {
	token := extractBearer(c.GetHeader("Authorization"))
	if h.gatewayToken != "" && token != h.gatewayToken {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid gateway token"})
		return
	}
	clusterName := c.GetHeader("X-Cluster-Name")
	if clusterName == "" {
		c.AbortWithStatusJSON(http.StatusBadRequest, gin.H{"error": "missing X-Cluster-Name"})
		return
	}
	cl, err := h.clusterSvc.GetByName(clusterName)
	if err != nil {
		c.AbortWithStatusJSON(http.StatusNotFound, gin.H{"error": "unknown cluster"})
		return
	}

	ws, err := h.upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		applog.Infof("gateway: upgrade failed for cluster %s: %v", clusterName, err)
		return
	}

	h.register(cl.ID, ws)
	if h.connectH != nil {
		h.connectH(c.Request.Context(), cl.ID)
	}
	defer h.unregister(cl.ID, ws)

	if err := h.clusterSvc.Heartbeat(cl.ID, true); err != nil {
		applog.Infof("gateway: mark online failed for cluster %s: %v", clusterName, err)
	}

	applog.Infof("gateway: cluster %s connected", clusterName)
	h.readLoop(c.Request.Context(), cl.ID, ws)
	applog.Infof("gateway: cluster %s disconnected", clusterName)
}

func (h *HubServer) register(clusterID uuid.UUID, ws *websocket.Conn) {
	h.mu.Lock()
	h.conns[clusterID] = ws
	h.mu.Unlock()
}

// unregister 移除连接。同一集群的新旧连接在滚动重启时会短暂并存：
// 旧连接的 readLoop 退出时绝不能把新连接的注册项一并删掉，否则会出现
// "心跳还在收、但 Dispatch 查不到连接"（或把派发写进已死的旧连接）。
// 因此这里只在自己仍是注册项时才删除。
func (h *HubServer) unregister(clusterID uuid.UUID, ws *websocket.Conn) {
	h.mu.Lock()
	owned := h.conns[clusterID] == ws
	if owned {
		delete(h.conns, clusterID)
	}
	h.mu.Unlock()
	if owned {
		_ = h.clusterSvc.Heartbeat(clusterID, false)
	}
}

// Dispatch sends an ApplyPipelineRunPayload to the Runner managing clusterID.
// It returns ErrNoRunner if that cluster has no live connection.
func (h *HubServer) Dispatch(ctx context.Context, clusterID uuid.UUID, payload *runnerapi.ApplyPipelineRunPayload) error {
	h.mu.RLock()
	ws, ok := h.conns[clusterID]
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

// Approve sends an ApproveTaskPayload to the Runner managing clusterID,
// relaying an approver's decision for a paused Approval task. It returns
// ErrNoRunner if that cluster has no live connection, so the caller can
// surface a clear "cluster offline, cannot deliver decision" error.
func (h *HubServer) Approve(ctx context.Context, clusterID uuid.UUID, payload *runnerapi.ApproveTaskPayload) error {
	h.mu.RLock()
	ws, ok := h.conns[clusterID]
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
// clusterID, relaying an operator's pause/promote/rollback command for a
// Release task's Rollout. It returns ErrNoRunner if that cluster has no live
// connection, so the caller can surface a clear "cluster offline" error.
func (h *HubServer) RolloutControl(ctx context.Context, clusterID uuid.UUID, payload *runnerapi.RolloutControlPayload) error {
	h.mu.RLock()
	ws, ok := h.conns[clusterID]
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

func (h *HubServer) readLoop(ctx context.Context, clusterID uuid.UUID, ws *websocket.Conn) {
	for {
		_, data, err := ws.ReadMessage()
		if err != nil {
			// Normal on clean disconnect; log at debug level in production.
			applog.Infof("gateway: cluster %s read error: %v", clusterID, err)
			return
		}
		var msg runnerapi.Message
		if err := json.Unmarshal(data, &msg); err != nil {
			applog.Infof("gateway: cluster %s bad frame: %v", clusterID, err)
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
				h.statusH(ctx, clusterID, &p)
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
			_ = h.clusterSvc.Heartbeat(clusterID, true)
		default:
			applog.Infof("gateway: cluster %s unhandled message type %q", clusterID, msg.Type)
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
