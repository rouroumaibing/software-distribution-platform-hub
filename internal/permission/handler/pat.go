package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/rouroumaibing/software-distribution-platform-hub/internal/common"
	"github.com/rouroumaibing/software-distribution-platform-hub/internal/middleware"
	permsvc "github.com/rouroumaibing/software-distribution-platform-hub/internal/permission/service"
)

// PATHandler exposes §6.4 personal access token endpoints: any authenticated
// user manages their OWN tokens (no extra permission action needed).
type PATHandler struct{ svc *permsvc.PATService }

func NewPATHandler(svc *permsvc.PATService) *PATHandler { return &PATHandler{svc: svc} }

func patSubject(c *gin.Context) string {
	s, _ := middleware.CurrentSubject(c)
	return s
}

// List godoc
//
//	@Summary	我的访问令牌（§6.4，掩码列表）
//	@Produce	json
//	@Success	200	{object}	common.Envelope
//	@Router		/me/tokens [get]
func (h *PATHandler) List(c *gin.Context) {
	items, err := h.svc.List(patSubject(c))
	if err != nil {
		common.Fail(c, http.StatusInternalServerError, err)
		return
	}
	common.OK(c, items)
}

// Create godoc
//
//	@Summary	创建访问令牌（明文仅此一次返回）
//	@Accept		json
//	@Param		body	body	permsvc.CreateTokenInput	true	"名称 + scopes(read|write) + 可选过期"
//	@Success	200	{object}	common.Envelope
//	@Router		/me/tokens [post]
func (h *PATHandler) Create(c *gin.Context) {
	var in permsvc.CreateTokenInput
	if err := c.ShouldBindJSON(&in); err != nil {
		common.Fail(c, http.StatusBadRequest, err)
		return
	}
	row, plaintext, err := h.svc.Create(patSubject(c), in)
	if err != nil {
		common.Fail(c, http.StatusBadRequest, err)
		return
	}
	common.OK(c, gin.H{"token": row, "plaintext": plaintext})
}

// Revoke godoc
//
//	@Summary	吊销访问令牌（高危：不可恢复）
//	@Success	200	{object}	common.Envelope
//	@Router		/me/tokens/{id} [delete]
func (h *PATHandler) Revoke(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		common.Fail(c, http.StatusBadRequest, err)
		return
	}
	if err := h.svc.Revoke(patSubject(c), id); err != nil {
		common.Fail(c, http.StatusInternalServerError, err)
		return
	}
	common.OK(c, gin.H{"revoked": true})
}
