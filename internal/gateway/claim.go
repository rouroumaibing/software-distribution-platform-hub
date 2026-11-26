package gateway

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	targetmodels "github.com/rouroumaibing/software-distribution-platform-hub/internal/target/models"
)

// ClaimedJob mirrors the dispatch payload handed to a Runner over the HTTP
// pull path. Declared here (not in run/service) so the gateway stays
// decoupled from the run service; the assembly layer converts.
type ClaimedJob struct {
	JobID         uuid.UUID       `json:"job_id"`
	PipelineRunID uuid.UUID       `json:"pipeline_run_id"`
	Payload       json.RawMessage `json:"payload"`
}

// ClaimHandler serves a Runner's claim for its target. Returns the jobs the
// caller now owns (already claimed against the push path via MarkDispatching
// occupancy); an empty slice means "nothing pending".
type ClaimHandler func(ctx context.Context, targetID uuid.UUID) ([]ClaimedJob, error)

// SetClaimHandler wires the pull-claim callback (nil = endpoint returns 503).
func (h *HubServer) SetClaimHandler(fn ClaimHandler) { h.claimH = fn }

// ServeClaim is the HTTP pull endpoint mounted at POST {gateway path}/claims.
// Identity follows the WS three-state scheme minus enroll-token consumption:
// a one-time enroll token must never be spent on a poll, so enroll-mode
// runners claim only after the WS handshake returned their per-target
// credential. An unknown self-reported target is a 404 — no HTTP auto-enroll.
//
// Any instance can serve the claim: the occupancy is settled atomically in
// the shared PG, so no coordination role check participates (§1 登记面多活).
func (h *HubServer) ServeClaim(c *gin.Context) {
	if h.claimH == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "claim endpoint not wired"})
		return
	}
	tg, ok := h.resolveClaimTarget(c)
	if !ok {
		return // response already written
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
	defer cancel()
	jobs, err := h.claimH(ctx, tg.ID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if jobs == nil {
		jobs = []ClaimedJob{}
	}
	c.JSON(http.StatusOK, gin.H{"jobs": jobs})
}

// resolveClaimTarget authenticates the claim request and resolves the target
// row. Identity sources: shared gateway token + X-Target-Name (legacy path),
// or a per-target credential looked up server-side.
func (h *HubServer) resolveClaimTarget(c *gin.Context) (*targetmodels.Target, bool) {
	token := extractBearer(c.GetHeader("Authorization"))
	targetName := c.GetHeader("X-Target-Name")

	if h.gatewayToken == "" || token == h.gatewayToken {
		if targetName == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "X-Target-Name required"})
			return nil, false
		}
		tg, err := h.targetSvc.GetByName(targetName)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusNotFound, gin.H{"error": "unknown target"})
			return nil, false
		}
		return tg, true
	}
	tg, err := h.targetSvc.GetByCredential(token)
	if err != nil || (targetName != "" && targetName != tg.Name) {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid gateway token"})
		return nil, false
	}
	return tg, true
}
