package platform

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/rouroumaibing/software-distribution-platform-hub/internal/common"
)

// Handler exposes platform settings (ui-12 §5.1 变更窗口 / §6.3 平台设置).
// Routes live on the platform-admin group — org:manage-level visibility.
type Handler struct{ store SettingStore }

func NewHandler(store SettingStore) *Handler { return &Handler{store: store} }

// GetChangeWindow godoc
//
//	@Summary	读取变更窗口配置（§5.1）
//	@Produce	json
//	@Success	200	{object}	common.Envelope{data=ChangeWindow}
//	@Router		/platform/settings/change-window [get]
func (h *Handler) GetChangeWindow(c *gin.Context) {
	cw, err := LoadChangeWindow(h.store)
	if err != nil {
		common.Fail(c, http.StatusInternalServerError, err)
		return
	}
	common.OK(c, cw)
}

// PutChangeWindow godoc
//
//	@Summary	保存变更窗口配置（§5.1 特性开关 + 时段）
//	@Accept		json
//	@Produce	json
//	@Param		body	body	ChangeWindow	true	"enabled=false 时窗口字段仅存档"
//	@Success	200	{object}	common.Envelope{data=ChangeWindow}
//	@Router		/platform/settings/change-window [put]
func (h *Handler) PutChangeWindow(c *gin.Context) {
	var cw ChangeWindow
	if err := c.ShouldBindJSON(&cw); err != nil {
		common.Fail(c, http.StatusBadRequest, err)
		return
	}
	if err := SaveChangeWindow(h.store, cw); err != nil {
		common.Fail(c, http.StatusBadRequest, err)
		return
	}
	common.OK(c, cw)
}
