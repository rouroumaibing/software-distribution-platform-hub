package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/rouroumaibing/software-distribution-platform-hub/internal/common"
	"github.com/rouroumaibing/software-distribution-platform-hub/internal/pipeline/models"
	"github.com/rouroumaibing/software-distribution-platform-hub/internal/pipeline/service"
)

// StageHandler intentionally skips common.RegisterCRUD — stage identity is
// (pipeline, name, sequence) and tasks are grouped under it, so the mutable
// surface is just rename/reorder (PUT /stages/:id) plus Create/List/Delete.
//
// 错误一律交 common.AbortWithError：它把 *APIError 按其 Code 落状态码、
// 把 gorm.ErrRecordNotFound 映成 404。父 pipeline 不存在/已软删时，
// service 层返回的是 404 域错误，早先用 Fail(500) 会把 404 塌成 500。
type StageHandler struct{ svc *service.StageService }

func NewStageHandler(svc *service.StageService) *StageHandler { return &StageHandler{svc: svc} }

func (h *StageHandler) RegisterRoutes(rg *gin.RouterGroup) {
	rg.POST("/pipelines/:id/stages", h.Create)
	rg.GET("/pipelines/:id/stages", h.List)
	rg.PUT("/stages/:id", h.Update)
	rg.DELETE("/stages/:id", h.Delete)
}

func (h *StageHandler) Create(c *gin.Context) {
	pipelineID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		common.AbortWithError(c, common.ErrBadRequest.WithError(err))
		return
	}
	var in models.PipelineStage
	if err := c.ShouldBindJSON(&in); err != nil {
		common.AbortWithError(c, common.ErrBadRequest.WithError(err))
		return
	}
	in.PipelineID = pipelineID
	if err := h.svc.Create(&in); err != nil {
		common.AbortWithError(c, err)
		return
	}
	common.Created(c, in)
}

func (h *StageHandler) List(c *gin.Context) {
	pipelineID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		common.AbortWithError(c, common.ErrBadRequest.WithError(err))
		return
	}
	items, err := h.svc.ListByPipeline(pipelineID)
	if err != nil {
		common.AbortWithError(c, err)
		return
	}
	common.OK(c, items)
}

func (h *StageHandler) Update(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		common.AbortWithError(c, common.ErrBadRequest.WithError(err))
		return
	}
	var in models.PipelineStage
	if err := c.ShouldBindJSON(&in); err != nil {
		common.AbortWithError(c, common.ErrBadRequest.WithError(err))
		return
	}
	// PipelineID 来自 URL 时不允许经 body 换绑;Update 本身也只拷贝 name/sequence。
	out, err := h.svc.Update(id, &in)
	if err != nil {
		common.AbortWithError(c, err)
		return
	}
	common.OK(c, out)
}

func (h *StageHandler) Delete(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		common.AbortWithError(c, common.ErrBadRequest.WithError(err))
		return
	}
	if err := h.svc.Delete(id); err != nil {
		common.AbortWithError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}
