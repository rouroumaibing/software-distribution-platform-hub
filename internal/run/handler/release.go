package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/rouroumaibing/software-distribution-platform-hub/internal/common"
	"github.com/rouroumaibing/software-distribution-platform-hub/internal/run/models"
	"github.com/rouroumaibing/software-distribution-platform-hub/internal/run/service"
)

// ReleaseHandler serves the /releases CRUD surface consumed by the console's
// release view. Releases are global (not component-scoped), so routes are
// registered on the versioned api group. Permission is left to the global
// auth middleware for now; release-level permission granularity is tracked
// under backlog P1-1 and will tighten this bare mount later.
type ReleaseHandler struct{ svc *service.ReleaseService }

func NewReleaseHandler(svc *service.ReleaseService) *ReleaseHandler {
	return &ReleaseHandler{svc: svc}
}

func (h *ReleaseHandler) RegisterRoutes(rg *gin.RouterGroup) {
	rg.POST("/releases", h.Create)
	rg.GET("/releases", h.List)
	rg.GET("/releases/:id", h.Get)
	rg.PUT("/releases/:id", h.Update)
	rg.DELETE("/releases/:id", h.Delete)
}

func (h *ReleaseHandler) Create(c *gin.Context) {
	var in models.RolloutRun
	if err := c.ShouldBindJSON(&in); err != nil {
		common.Fail(c, http.StatusBadRequest, err)
		return
	}
	if err := h.svc.Create(&in); err != nil {
		common.Fail(c, http.StatusInternalServerError, err)
		return
	}
	common.Created(c, in)
}

func (h *ReleaseHandler) List(c *gin.Context) {
	p := common.ParsePagination(c)
	var prID *uuid.UUID
	if v := c.Query("pipelineRunId"); v != "" {
		if id, err := uuid.Parse(v); err == nil {
			prID = &id
		}
	}
	items, total, err := h.svc.List(p, prID)
	if err != nil {
		common.Fail(c, http.StatusInternalServerError, err)
		return
	}
	common.OKPaged(c, items, total, p)
}

func (h *ReleaseHandler) Get(c *gin.Context) {
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

func (h *ReleaseHandler) Update(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		common.Fail(c, http.StatusBadRequest, err)
		return
	}
	existing, err := h.svc.Get(id)
	if err != nil {
		common.Fail(c, http.StatusNotFound, err)
		return
	}
	// Bind onto the loaded row so only the fields present in the JSON body are
	// overwritten; the FK and omitted fields are preserved.
	if err := c.ShouldBindJSON(existing); err != nil {
		common.Fail(c, http.StatusBadRequest, err)
		return
	}
	if err := h.svc.Update(id, existing); err != nil {
		common.Fail(c, http.StatusInternalServerError, err)
		return
	}
	common.OK(c, existing)
}

func (h *ReleaseHandler) Delete(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
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
