package handler

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/rouroumaibing/software-distribution-platform-hub/internal/common"
	"github.com/rouroumaibing/software-distribution-platform-hub/internal/target/models"
	"github.com/rouroumaibing/software-distribution-platform-hub/internal/target/service"
)

type TargetHandler struct {
	svc *service.TargetService
	// ops records install/upgrade operations (§9.9); runnerVersion is the
	// target version read from the package-versions matrix (§9.10).
	ops           *service.AgentOpService
	runnerVersion string
	// bundle issues runner install bundles (ADR-0002); nil => endpoint 501.
	bundle *service.JoinBundleIssuer
}

func NewTargetHandler(svc *service.TargetService) *TargetHandler { return &TargetHandler{svc: svc} }

// SetAgentOps wires the agent-operation recorder and the runner version to
// install/upgrade with (optional injection, same pattern as service setters).
func (h *TargetHandler) SetAgentOps(ops *service.AgentOpService, runnerVersion string) {
	h.ops = ops
	h.runnerVersion = runnerVersion
}

// SetJoinBundle wires the join-bundle issuer (ADR-0002). Optional: without it
// (or without HUB_PUBLIC_URL) the endpoint reports 501 instead of guessing.
func (h *TargetHandler) SetJoinBundle(b *service.JoinBundleIssuer) {
	h.bundle = b
}

func (h *TargetHandler) RegisterRoutes(rg *gin.RouterGroup) {
	common.RegisterCRUD[models.Target](rg, "/targets", h.svc)
	rg.GET("/targets", h.List)
	// One-time bootstrap token for enrolling a Runner Agent (§9.9).
	rg.POST("/targets/:id/enroll-token", h.EnrollToken)
	// Runner install bundle (ADR-0002): URLs + CA + fingerprint + enroll token.
	rg.POST("/targets/:id/join-bundle", h.JoinBundle)
	// 接入编排 (§9.9): queue a runner install / upgrade against the version
	// matrix; execution is the Runner's job (hub has no client-go).
	rg.POST("/targets/:id/install", h.Install)
	rg.POST("/targets/:id/upgrade", h.Upgrade)
}

func (h *TargetHandler) List(c *gin.Context) {
	p := common.ParsePagination(c)
	items, total, err := h.svc.List(p)
	if err != nil {
		common.Fail(c, http.StatusInternalServerError, err)
		return
	}
	common.OKPaged(c, items, total, p)
}

func (h *TargetHandler) EnrollToken(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		common.Fail(c, http.StatusBadRequest, err)
		return
	}
	token, err := h.svc.GenerateEnrollToken(id)
	if err != nil {
		common.AbortWithError(c, err)
		return
	}
	common.OK(c, gin.H{"targetId": id.String(), "enrollToken": token, "expiresIn": "24h"})
}

// JoinBundle godoc
// @Summary Issue a runner install bundle for the target
// @Description Returns gatewayUrl / apiBaseUrl / caPem / caFingerprint (RFC7469 SPKI SHA256) / per-target one-time enrollToken (ADR-0002). Consumed on a trusted channel (operator session or provisioning pipeline), never by the target itself. 501 when the hub lacks HUB_PUBLIC_URL or a readable TLS CA.
// @Tags targets
// @Produce json
// @Param id path string true "Target ID (UUID)"
// @Success 200 {object} common.Envelope
// @Failure 400 {object} common.Envelope
// @Failure 404 {object} common.Envelope
// @Failure 501 {object} common.Envelope
// @Router /targets/{id}/join-bundle [post]
func (h *TargetHandler) JoinBundle(c *gin.Context) {
	if h.bundle == nil {
		common.Fail(c, http.StatusNotImplemented,
			fmt.Errorf("join-bundle unavailable: HUB_PUBLIC_URL / TLS CA not configured (ADR-0002)"))
		return
	}
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		common.Fail(c, http.StatusBadRequest, err)
		return
	}
	bundle, err := h.bundle.Issue(id)
	if err != nil {
		if errors.Is(err, service.ErrJoinBundleUnavailable) {
			common.Fail(c, http.StatusNotImplemented, err)
			return
		}
		common.AbortWithError(c, err)
		return
	}
	common.OK(c, bundle)
}

// Install godoc
// @Summary Queue a runner install on the target
// @Description Records an agent_ops entry (queued) carrying the runner version from the package-versions matrix; execution is the Runner's job (§9.9).
// @Tags targets
// @Produce json
// @Param id path string true "Target ID (UUID)"
// @Success 202 {object} common.Envelope
// @Failure 400 {object} common.Envelope
// @Failure 404 {object} common.Envelope
// @Router /targets/{id}/install [post]
func (h *TargetHandler) Install(c *gin.Context) { h.queueAgentOp(c, models.AgentOpInstall) }

// Upgrade godoc
// @Summary Queue a runner upgrade on the target
// @Description Records an agent_ops entry (queued) carrying the runner version from the package-versions matrix; execution is the Runner's job (§9.9).
// @Tags targets
// @Produce json
// @Param id path string true "Target ID (UUID)"
// @Success 202 {object} common.Envelope
// @Failure 400 {object} common.Envelope
// @Failure 404 {object} common.Envelope
// @Router /targets/{id}/upgrade [post]
func (h *TargetHandler) Upgrade(c *gin.Context) { h.queueAgentOp(c, models.AgentOpUpgrade) }

func (h *TargetHandler) queueAgentOp(c *gin.Context, opType string) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		common.Fail(c, http.StatusBadRequest, err)
		return
	}
	if h.ops == nil {
		common.Fail(c, http.StatusNotImplemented,
			fmt.Errorf("agent op store not wired (install/upgrade unavailable)"))
		return
	}
	// 404 on an unknown target before recording anything.
	if _, err := h.svc.Get(id); err != nil {
		common.AbortWithError(c, err)
		return
	}
	op := &models.AgentOp{TargetID: id, OpType: opType, Detail: h.runnerVersion}
	if err := h.ops.Create(op); err != nil {
		common.AbortWithError(c, err)
		return
	}
	c.JSON(http.StatusAccepted, common.Envelope{Data: op})
}
