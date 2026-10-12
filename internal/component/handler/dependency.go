package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/rouroumaibing/software-distribution-platform-hub/internal/common"
	"github.com/rouroumaibing/software-distribution-platform-hub/internal/component/service"
)

// DependencyHandler exposes §6.1 依赖图 endpoints on /components/:id/dependencies.
type DependencyHandler struct{ svc *service.DependencyService }

func NewDependencyHandler(svc *service.DependencyService) *DependencyHandler {
	return &DependencyHandler{svc: svc}
}

func depIDs(c *gin.Context) (uuid.UUID, uuid.UUID, bool) {
	componentID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		common.Fail(c, http.StatusBadRequest, err)
		return uuid.Nil, uuid.Nil, false
	}
	var body struct {
		DependsOnID string `json:"dependsOnId" binding:"required"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		common.Fail(c, http.StatusBadRequest, err)
		return uuid.Nil, uuid.Nil, false
	}
	dependsOn, err := uuid.Parse(body.DependsOnID)
	if err != nil {
		common.Fail(c, http.StatusBadRequest, err)
		return uuid.Nil, uuid.Nil, false
	}
	return componentID, dependsOn, true
}

// Add godoc
//
//	@Summary	声明依赖（§6.1）
//	@Accept		json
//	@Param		body	body	object{dependsOnId=string}	true	"被依赖组件"
//	@Success	200		{object}	common.Envelope
//	@Router		/components/{id}/dependencies [post]
func (h *DependencyHandler) Add(c *gin.Context) {
	componentID, dependsOn, ok := depIDs(c)
	if !ok {
		return
	}
	edge, err := h.svc.Add(componentID, dependsOn)
	if err != nil {
		common.Fail(c, http.StatusConflict, err)
		return
	}
	common.OK(c, edge)
}

// Remove godoc
//
//	@Summary	解除依赖（§6.1）
//	@Accept		json
//	@Param		body	body	object{dependsOnId=string}	true	"被依赖组件"
//	@Success	200	{object}	common.Envelope
//	@Router		/components/{id}/dependencies [delete]
func (h *DependencyHandler) Remove(c *gin.Context) {
	componentID, dependsOn, ok := depIDs(c)
	if !ok {
		return
	}
	if err := h.svc.Remove(componentID, dependsOn); err != nil {
		common.Fail(c, http.StatusInternalServerError, err)
		return
	}
	common.OK(c, gin.H{"removed": true})
}

// List godoc
//
//	@Summary	依赖列表（§6.1）
//	@Produce	json
//	@Param		direction	query	string	false	"upstream（默认：我依赖谁）| downstream（谁依赖我）"
//	@Success	200			{object}	common.Envelope
//	@Router		/components/{id}/dependencies [get]
func (h *DependencyHandler) List(c *gin.Context) {
	componentID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		common.Fail(c, http.StatusBadRequest, err)
		return
	}
	upstream := c.DefaultQuery("direction", "upstream") != "downstream"
	items, err := h.svc.List(componentID, upstream)
	if err != nil {
		common.Fail(c, http.StatusInternalServerError, err)
		return
	}
	common.OK(c, items)
}
