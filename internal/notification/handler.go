package notification

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/rouroumaibing/software-distribution-platform-hub/internal/common"
)

// Handler 暴露通知中心端点。鉴权由路由层（authed group）保证。
type Handler struct {
	svc *Service
}

func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

// List 返回当前用户的通知流。
//
//	@Summary	通知中心列表
//	@Tags		notification
//	@Produce	json
//	@Success	200	{object}	common.Envelope{data=[]Notification}
//	@Router		/notifications [get]
func (h *Handler) List(c *gin.Context) {
	items, err := h.svc.List(c.Request.Context(), 50)
	if err != nil {
		common.Fail(c, http.StatusInternalServerError, err)
		return
	}
	common.OK(c, gin.H{"data": items, "total": len(items)})
}
