package handler

import (
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/rouroumaibing/software-distribution-platform-hub/internal/common"
	"github.com/rouroumaibing/software-distribution-platform-hub/internal/permission/repository"
)

// AuditLogHandler serves the read side of the audit sink (STATUS #19 /
// CONSOLE-UI-GAPS §3.1). Writes go through AuditMiddleware only; this handler
// is read-only.
//
// 鉴权约束（不是裸 HTTP 端点）：audit_log 覆盖平台上**所有**主体与资源的变更
// 留痕，读到它 = 读到全平台操作记录。因此 main.go 里本 handler 只允许挂
// platformGroup —— 开启鉴权后经过 RequirePlatformPermission(user:manage)，
// 与 /platform-roles 同一条平台级 RBAC 链；dev 模式（auth==nil）才随组裸挂。
// audit_route_test.go 用注册分组隔离断言钉住这一点，防止将来被挪到裸 api。
type AuditLogHandler struct {
	repo *repository.AuditLogRepository
}

func NewAuditLogHandler(repo *repository.AuditLogRepository) *AuditLogHandler {
	return &AuditLogHandler{repo: repo}
}

func (h *AuditLogHandler) RegisterRoutes(rg *gin.RouterGroup) {
	rg.GET("/audit-logs", h.List)
}

// List returns audit rows newest-first. Filters (all optional):
//
//	subject       — exact subject (token sub or /group)
//	actionPrefix  — action prefix match, e.g. approval. → approval.approve / approval.reject
//	resourceType  — exact resource type
//	resourceId    — exact resource id
//	since/until   — RFC3339 time window (inclusive)
//	limit         — 1..500, default 100 (clamped in the repository too)
func (h *AuditLogHandler) List(c *gin.Context) {
	q := repository.AuditQuery{
		Subject:      c.Query("subject"),
		ActionPrefix: c.Query("actionPrefix"),
		ResourceType: c.Query("resourceType"),
		ResourceID:   c.Query("resourceId"),
	}
	if v := c.Query("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n <= 0 {
			common.Fail(c, http.StatusBadRequest, fmt.Errorf("limit 必须是正整数"))
			return
		}
		q.Limit = n
	}
	if v := c.Query("since"); v != "" {
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			common.Fail(c, http.StatusBadRequest, fmt.Errorf("since 必须是 RFC3339 时间（如 2026-10-08T00:00:00Z）"))
			return
		}
		q.Since = t
	}
	if v := c.Query("until"); v != "" {
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			common.Fail(c, http.StatusBadRequest, fmt.Errorf("until 必须是 RFC3339 时间（如 2026-10-08T00:00:00Z）"))
			return
		}
		q.Until = t
	}
	rows, err := h.repo.Query(q)
	if err != nil {
		failPermission(c, err)
		return
	}
	common.OK(c, rows)
}
