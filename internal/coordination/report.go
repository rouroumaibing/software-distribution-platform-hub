package coordination

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

// Report shapes (§4.4): producers report to the master over pod-to-pod HTTP
// and get their shard assignment back; only the master serves the endpoint —
// anyone else answers 409 with the instance-lock holder so the producer can
// follow the new master on its next tick.
type ReportRequest struct {
	PodIP     string `json:"podIP"`
	ClusterID string `json:"clusterID"`
}

type ReportResponse struct {
	Shards       []int  `json:"shards"`
	MappingEpoch uint64 `json:"mappingEpoch"`
}

type ConflictResponse struct {
	Master string `json:"master"`
}

// NotMasterError is returned by the report client when the peer refused
// because it is not the master; Master carries the peer's observed holder.
type NotMasterError struct {
	Master string
}

// Error renders the refusal with the routing hint.
func (e *NotMasterError) Error() string {
	return fmt.Sprintf("report rejected: peer is not master (holder=%s)", e.Master)
}

// ReportHandler serves the internal report endpoint.
type ReportHandler struct {
	coord *Coordinator
}

// NewReportHandler builds the handler on a coordinator (nil means
// coordination is disabled — the endpoint answers 501).
func NewReportHandler(coord *Coordinator) *ReportHandler { return &ReportHandler{coord: coord} }

// RegisterRoutes mounts the endpoint on the bare engine. The route stays
// pod-network-only: the external gateway routes /api and the gateway path,
// never /internal — the network boundary is the security boundary (§5); an
// app-layer token is the recorded follow-up should that assumption break.
func (h *ReportHandler) RegisterRoutes(rg gin.IRouter) {
	rg.POST("/internal/coordination/report", h.handle)
}

func (h *ReportHandler) handle(c *gin.Context) {
	if h.coord == nil {
		c.JSON(http.StatusNotImplemented, gin.H{"error": "coordination disabled"})
		return
	}
	var req ReportRequest
	if err := c.ShouldBindJSON(&req); err != nil || req.PodIP == "" || req.ClusterID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "podIP and clusterID are required"})
		return
	}
	// 仅 master 可收上报：先自证（§2.3 双条件），非 master 一律 409 并指路。
	if h.coord.Role() != RoleMaster {
		c.JSON(http.StatusConflict, ConflictResponse{Master: h.coord.MasterHint()})
		return
	}
	if req.ClusterID != h.coord.opts.ClusterID {
		// standby 集群实例不应上报（producer 先判集群级 leader）；防御性拒绝。
		c.JSON(http.StatusConflict, ConflictResponse{Master: h.coord.MasterHint()})
		return
	}
	shards, epoch := h.coord.AssignmentFor(req.PodIP)
	c.JSON(http.StatusOK, ReportResponse{Shards: shards, MappingEpoch: epoch})
}

// reportClient posts reports to the master pod.
type reportClient struct {
	port int
	hc   *http.Client
}

func newReportClient(port int) *reportClient {
	return &reportClient{
		port: port,
		hc:   &http.Client{Timeout: 2 * time.Second},
	}
}

// Report posts the report to masterIP. A 409 comes back as *NotMasterError
// carrying the peer's observed instance-lock holder (§4.4 routing hint).
func (rc *reportClient) Report(ctx context.Context, masterIP string, req ReportRequest) ([]int, uint64, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, 0, err
	}
	url := fmt.Sprintf("http://%s:%d/internal/coordination/report", masterIP, rc.port)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, 0, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	resp, err := rc.hc.Do(httpReq)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusOK:
		var out ReportResponse
		if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
			return nil, 0, err
		}
		return out.Shards, out.MappingEpoch, nil
	case http.StatusConflict:
		var out ConflictResponse
		_ = json.NewDecoder(resp.Body).Decode(&out)
		return nil, 0, &NotMasterError{Master: out.Master}
	default:
		return nil, 0, fmt.Errorf("report endpoint returned %d", resp.StatusCode)
	}
}
