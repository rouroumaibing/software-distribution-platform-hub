package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/rouroumaibing/software-distribution-platform-hub/internal/common"
	"github.com/rouroumaibing/software-distribution-platform-hub/internal/middleware"
	"github.com/rouroumaibing/software-distribution-platform-hub/internal/pipeline/models"
	"github.com/rouroumaibing/software-distribution-platform-hub/internal/pipeline/service"
)

type PipelineHandler struct{ svc *service.PipelineService }

func NewPipelineHandler(svc *service.PipelineService) *PipelineHandler {
	return &PipelineHandler{svc: svc}
}

// RegisterRoutes exposes the pipeline CRUD surface consumed by the console's
// createCrud('/pipelines'):
//   - POST   /pipelines              create
//   - GET    /pipelines/:id          get
//   - PUT    /pipelines/:id          update
//   - DELETE /pipelines/:id          delete
//   - GET    /components/:componentId/pipelines   parent-scoped list
//
// These are written out (rather than via common.RegisterCRUD) so Create can
// stamp CreatedBy from the authenticated user and so the console's exact
// path contract stays explicit.
func (h *PipelineHandler) RegisterRoutes(rg *gin.RouterGroup) {
	rg.POST("/pipelines", h.Create)
	rg.GET("/pipelines/:id", h.Get)
	rg.PUT("/pipelines/:id", h.Update)
	rg.DELETE("/pipelines/:id", h.Delete)
	rg.GET("/components/:id/pipelines", h.ListByComponent)
}

func (h *PipelineHandler) Create(c *gin.Context) {
	var in models.Pipeline
	if err := c.ShouldBindJSON(&in); err != nil {
		common.Fail(c, http.StatusBadRequest, err)
		return
	}
	if createdBy, ok := middleware.CurrentUserID(c); ok {
		in.CreatedBy = createdBy.String()
	}
	if err := h.svc.Create(&in); err != nil {
		common.Fail(c, http.StatusInternalServerError, err)
		return
	}
	common.Created(c, in)
}

func (h *PipelineHandler) Get(c *gin.Context) {
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

func (h *PipelineHandler) Update(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		common.Fail(c, http.StatusBadRequest, err)
		return
	}
	var in models.Pipeline
	if err := c.ShouldBindJSON(&in); err != nil {
		common.Fail(c, http.StatusBadRequest, err)
		return
	}
	if err := h.svc.Update(id, &in); err != nil {
		common.Fail(c, http.StatusInternalServerError, err)
		return
	}
	common.OK(c, in)
}

func (h *PipelineHandler) Delete(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		common.Fail(c, http.StatusBadRequest, err)
		return
	}
	if err := h.svc.Delete(id); err != nil {
		// Propagate the domain error's HTTP status (e.g. 409 when run
		// history still references the pipeline) instead of collapsing to 500.
		if ae, ok := err.(*common.APIError); ok {
			common.Fail(c, ae.Code, err)
			return
		}
		common.Fail(c, http.StatusInternalServerError, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *PipelineHandler) ListByComponent(c *gin.Context) {
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
