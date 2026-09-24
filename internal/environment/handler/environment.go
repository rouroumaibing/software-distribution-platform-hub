package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/rouroumaibing/software-distribution-platform-hub/internal/common"
	"github.com/rouroumaibing/software-distribution-platform-hub/internal/environment/models"
	"github.com/rouroumaibing/software-distribution-platform-hub/internal/environment/service"
)

type EnvironmentHandler struct{ svc *service.EnvironmentService }

func NewEnvironmentHandler(svc *service.EnvironmentService) *EnvironmentHandler {
	return &EnvironmentHandler{svc: svc}
}

func (h *EnvironmentHandler) RegisterRoutes(rg *gin.RouterGroup) {
	common.RegisterCRUD[models.Environment](rg, "/environments", h.svc)
	rg.GET("/components/:id/environments", h.ListByComponent)
	// Connection test (§7.12.5): per-dimension checklist, persisted on the env.
	rg.POST("/environments/:id/test", h.Test)
	// Direct-exec (§9.5): validates and queues an agent operation; execution
	// is the Runner's job (hub has no client-go).
	rg.POST("/environments/:id/exec", h.Exec)
}

func (h *EnvironmentHandler) ListByComponent(c *gin.Context) {
	componentID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		common.Fail(c, http.StatusBadRequest, err)
		return
	}
	p := common.ParsePagination(c)
	items, total, err := h.svc.ListByComponent(componentID, p)
	if err != nil {
		common.Fail(c, http.StatusInternalServerError, err)
		return
	}
	common.OKPaged(c, items, total, p)
}

func (h *EnvironmentHandler) Test(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		common.Fail(c, http.StatusBadRequest, err)
		return
	}
	report, err := h.svc.Test(id)
	if err != nil {
		common.AbortWithError(c, err)
		return
	}
	common.OK(c, report)
}

// Exec godoc
// @Summary Queue a direct-exec operation on the environment's target
// @Description Validates the request and records an agent_ops entry (queued); execution is performed by the Runner / direct-connect executor, not the hub (no client-go).
// @Tags environments
// @Accept json
// @Produce json
// @Param id path string true "Environment ID (UUID)"
// @Param body body service.ExecRequest true "command or script (at least one)"
// @Success 202 {object} common.Envelope
// @Failure 400 {object} common.Envelope
// @Failure 404 {object} common.Envelope
// @Router /environments/{id}/exec [post]
func (h *EnvironmentHandler) Exec(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		common.Fail(c, http.StatusBadRequest, err)
		return
	}
	var in service.ExecRequest
	if err := c.ShouldBindJSON(&in); err != nil {
		common.AbortWithError(c, common.ErrBadRequest.WithError(err))
		return
	}
	op, err := h.svc.Exec(id, in)
	if err != nil {
		common.AbortWithError(c, err)
		return
	}
	c.JSON(http.StatusAccepted, common.Envelope{Data: op})
}
