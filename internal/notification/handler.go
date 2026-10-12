package notification

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/rouroumaibing/software-distribution-platform-hub/internal/common"
	"github.com/rouroumaibing/software-distribution-platform-hub/internal/middleware"
	"github.com/rouroumaibing/software-distribution-platform-hub/internal/notification/models"
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

// GetSubscription godoc
//
//	@Summary	我的通知订阅（§5.2）
//	@Produce	json
//	@Success	200	{object}	common.Envelope{data=models.Subscription}
//	@Router		/notifications/subscription [get]
func (h *Handler) GetSubscription(c *gin.Context) {
	subject := subjectOf(c)
	if subject == "" {
		common.Fail(c, http.StatusUnauthorized, errors.New("subject required"))
		return
	}
	sub, err := h.svc.SubscriptionOf(subject)
	if err != nil {
		common.Fail(c, http.StatusInternalServerError, err)
		return
	}
	common.OK(c, sub)
}

// PutSubscription godoc
//
//	@Summary	更新我的通知订阅（邮箱渠道 + 事件白名单 + 总开关）
//	@Accept		json
//	@Produce	json
//	@Param		body	body	models.Subscription	true	"subject 字段忽略，取身份主体"
//	@Success	200	{object}	common.Envelope{data=models.Subscription}
//	@Router		/notifications/subscription [put]
func (h *Handler) PutSubscription(c *gin.Context) {
	subject := subjectOf(c)
	if subject == "" {
		common.Fail(c, http.StatusUnauthorized, errors.New("subject required"))
		return
	}
	var in models.Subscription
	if err := c.ShouldBindJSON(&in); err != nil {
		common.Fail(c, http.StatusBadRequest, err)
		return
	}
	if in.Email != "" && !strings.Contains(in.Email, "@") {
		common.Fail(c, http.StatusBadRequest, errors.New("email 格式非法"))
		return
	}
	if err := validateEvents(in.Events); err != nil {
		common.Fail(c, http.StatusBadRequest, err)
		return
	}
	in.Subject = subject // 防越权：只允许改自己的订阅
	if err := h.svc.UpsertSubscription(&in); err != nil {
		common.Fail(c, http.StatusInternalServerError, err)
		return
	}
	common.OK(c, in)
}

// supportedEvents is the event-type whitelist for §5.2 subscriptions.
var supportedEvents = map[string]bool{
	"approval_required": true,
	"run_finished":      true,
	"run_failed":        true,
}

func validateEvents(events string) error {
	if strings.TrimSpace(events) == "" {
		return nil // 默认 approval_required
	}
	for _, e := range strings.Split(events, ",") {
		e = strings.TrimSpace(e)
		if e != "" && !supportedEvents[e] {
			return fmt.Errorf("不支持的事件类型 %q（可选 approval_required/run_finished/run_failed）", e)
		}
	}
	return nil
}
