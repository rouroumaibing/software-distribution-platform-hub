package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/rouroumaibing/software-distribution-platform-hub/internal/common"
	"github.com/rouroumaibing/software-distribution-platform-hub/internal/pipeline/models"
	"github.com/rouroumaibing/software-distribution-platform-hub/internal/pipeline/service"
)

// TaskTemplateHandler 的错误同样走 common.AbortWithError（原因见 stage.go）：
// stage 不存在或父 pipeline 已软删时返回 404，而不是把 FK 报错塌成 500。
type TaskTemplateHandler struct{ svc *service.TaskTemplateService }

func NewTaskTemplateHandler(svc *service.TaskTemplateService) *TaskTemplateHandler {
	return &TaskTemplateHandler{svc: svc}
}

func (h *TaskTemplateHandler) RegisterRoutes(rg *gin.RouterGroup) {
	rg.POST("/stages/:stageId/tasks", h.Create)
	rg.GET("/stages/:stageId/tasks", h.List)
	rg.PUT("/tasks/:id", h.Update)
	rg.DELETE("/tasks/:id", h.Delete)
}

func (h *TaskTemplateHandler) Create(c *gin.Context) {
	stageID, err := uuid.Parse(c.Param("stageId"))
	if err != nil {
		common.AbortWithError(c, common.ErrBadRequest.WithError(err))
		return
	}
	var in models.PipelineTaskTemplate
	if err := c.ShouldBindJSON(&in); err != nil {
		common.AbortWithError(c, common.ErrBadRequest.WithError(err))
		return
	}
	in.StageID = stageID
	if err := h.svc.Create(&in); err != nil {
		common.AbortWithError(c, err)
		return
	}
	common.Created(c, in)
}

func (h *TaskTemplateHandler) List(c *gin.Context) {
	stageID, err := uuid.Parse(c.Param("stageId"))
	if err != nil {
		common.AbortWithError(c, common.ErrBadRequest.WithError(err))
		return
	}
	items, err := h.svc.ListByStage(stageID)
	if err != nil {
		common.AbortWithError(c, err)
		return
	}
	common.OK(c, items)
}

func (h *TaskTemplateHandler) Update(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		common.AbortWithError(c, common.ErrBadRequest.WithError(err))
		return
	}
	var in models.PipelineTaskTemplate
	if err := c.ShouldBindJSON(&in); err != nil {
		common.AbortWithError(c, common.ErrBadRequest.WithError(err))
		return
	}
	if err := h.svc.Update(id, &in); err != nil {
		common.AbortWithError(c, err)
		return
	}
	common.OK(c, in)
}

func (h *TaskTemplateHandler) Delete(c *gin.Context) {
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
