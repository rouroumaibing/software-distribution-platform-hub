package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/rouroumaibing/software-distribution-platform-hub/internal/common"
	"github.com/rouroumaibing/software-distribution-platform-hub/internal/component/models"
	"github.com/rouroumaibing/software-distribution-platform-hub/internal/component/service"
	"github.com/rouroumaibing/software-distribution-platform-hub/internal/middleware"
)

// ComponentConfigHandler is hand-written (not common.RegisterCRUD) because
// Upsert needs the caller's identity for the audit log, and there's no
// plain Update — a config key is always create-or-overwrite.
type ComponentConfigHandler struct {
	svc *service.ComponentConfigService
}

func NewComponentConfigHandler(svc *service.ComponentConfigService) *ComponentConfigHandler {
	return &ComponentConfigHandler{svc: svc}
}

func (h *ComponentConfigHandler) RegisterRoutes(rg *gin.RouterGroup) {
	rg.GET("/components/:id/configs", h.List)
	rg.PUT("/components/:id/configs/:key", h.Upsert)
	rg.DELETE("/components/:id/configs/:key", h.Delete)
}

func (h *ComponentConfigHandler) List(c *gin.Context) {
	componentID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		common.Fail(c, http.StatusBadRequest, err)
		return
	}
	var envID *uuid.UUID
	if raw := c.Query("environmentId"); raw != "" {
		id, err := uuid.Parse(raw)
		if err != nil {
			common.Fail(c, http.StatusBadRequest, err)
			return
		}
		envID = &id
	}
	p := common.ParsePagination(c)
	items, total, err := h.svc.List(componentID, envID, p)
	if err != nil {
		common.Fail(c, http.StatusInternalServerError, err)
		return
	}
	common.OKPaged(c, items, total, p)
}

func (h *ComponentConfigHandler) Upsert(c *gin.Context) {
	componentID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		common.Fail(c, http.StatusBadRequest, err)
		return
	}
	var in models.ComponentConfig
	if err := c.ShouldBindJSON(&in); err != nil {
		common.Fail(c, http.StatusBadRequest, err)
		return
	}
	in.ComponentID = componentID
	in.Key = c.Param("key")

	// changedBy 取 token `sub`（§5.3）—— D3 之后没有本地用户 id 可用。
	var changedBy *string
	if sub, ok := middleware.CurrentSubject(c); ok {
		changedBy = &sub
	}
	if err := h.svc.Upsert(&in, changedBy); err != nil {
		common.Fail(c, http.StatusInternalServerError, err)
		return
	}
	common.OK(c, in)
}

func (h *ComponentConfigHandler) Delete(c *gin.Context) {
	componentID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		common.Fail(c, http.StatusBadRequest, err)
		return
	}
	key := c.Param("key")
	// environmentId 匹配环境级覆盖行;不传则删除全局默认行(与 List 的语义一致)。
	var envID *uuid.UUID
	if raw := c.Query("environmentId"); raw != "" {
		id, err := uuid.Parse(raw)
		if err != nil {
			common.Fail(c, http.StatusBadRequest, err)
			return
		}
		envID = &id
	}
	cfg, err := h.svc.GetByKey(componentID, key, envID)
	if err != nil {
		common.Fail(c, http.StatusNotFound, err)
		return
	}
	var changedBy *string
	if sub, ok := middleware.CurrentSubject(c); ok {
		changedBy = &sub
	}
	if err := h.svc.Delete(cfg.ID, changedBy); err != nil {
		common.Fail(c, http.StatusInternalServerError, err)
		return
	}
	c.Status(http.StatusNoContent)
}
