package notification

import (
	"errors"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/rouroumaibing/software-distribution-platform-hub/internal/common"
	"github.com/rouroumaibing/software-distribution-platform-hub/internal/middleware"
)

// Handler 暴露通知中心端点。鉴权由路由层（authed group）保证。
type Handler struct {
	svc *Service
}

func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

// subjectOf reads the RBAC subject via middleware.CurrentSubject — the same
// single identity key bindings/audit use (token sub, or the dev pseudo-subject
// in dev mode). Empty means the identity middleware never ran (public route).
func subjectOf(c *gin.Context) string {
	s, _ := middleware.CurrentSubject(c)
	return s
}

// List 返回当前用户的通知流 + 服务端计算的未读数。
//
//	@Summary	通知中心列表
//	@Tags		notification
//	@Produce	json
//	@Success	200	{object}	common.Envelope{data=[]Notification}
//	@Router		/notifications [get]
func (h *Handler) List(c *gin.Context) {
	items, unread, err := h.svc.List(c.Request.Context(), subjectOf(c), 50)
	if err != nil {
		common.Fail(c, http.StatusInternalServerError, err)
		return
	}
	common.OK(c, gin.H{"data": items, "total": len(items), "unreadCount": unread})
}

// ReadMark godoc
//
//	@Summary	推进通知已读游标
//	@Tags		notification
//	@Accept		json
//	@Produce	json
//	@Param		body	body	models.ReadMark	false	"空 body = 服务端当前时间"
//	@Success	200		{object}	common.Envelope
//	@Router		/notifications/read-mark [post]
func (h *Handler) ReadMark(c *gin.Context) {
	var body struct {
		ReadAt *time.Time `json:"readAt"`
	}
	_ = c.ShouldBindJSON(&body) // 空 body 合法：默认服务端 now
	subject := subjectOf(c)
	if subject == "" {
		common.Fail(c, http.StatusUnauthorized, errors.New("subject required"))
		return
	}
	at := time.Now()
	if body.ReadAt != nil {
		at = *body.ReadAt
	}
	if err := h.svc.MarkRead(subject, at); err != nil {
		if errors.Is(err, errReadStoreUnavailable) {
			common.Fail(c, http.StatusServiceUnavailable, err)
			return
		}
		common.Fail(c, http.StatusInternalServerError, err)
		return
	}
	common.OK(c, gin.H{"lastReadAt": at})
}
