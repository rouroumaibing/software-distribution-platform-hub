package handler

import (
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/rouroumaibing/software-distribution-platform-hub/internal/common"
	"github.com/rouroumaibing/software-distribution-platform-hub/internal/middleware"
	"github.com/rouroumaibing/software-distribution-platform-hub/internal/permission/repository"
)

// AuditLogHandler serves the read side of the audit sink (STATUS #19 /
// CONSOLE-UI-GAPS §3.1). Writes go through AuditMiddleware only; this handler
// is read-only.
//
// 鉴权约束（不是裸 HTTP 端点）：audit_log 覆盖平台上**所有**主体与资源的变更
// 留痕。挂载纪律（2026-10-11 组件级审计下沉后）：main.go 里本 handler 挂在
// 经 RequireAuditAccess 的分组上 —— 平台管理员（user:manage）全量可查；
// 组件管理员（component:manage）须携带 ?componentId=，且 handler 在此把查询
// **强制**钳到该组件（中间件判定 + handler 过滤双保险，纵深防御）。
// audit_route_test.go 钉住路由存在性；分组结构由 main.go 保证。
type AuditLogHandler struct {
	repo *repository.AuditLogRepository
}

func NewAuditLogHandler(repo *repository.AuditLogRepository) *AuditLogHandler {
	return &AuditLogHandler{repo: repo}
}

func (h *AuditLogHandler) RegisterRoutes(rg *gin.RouterGroup) {
	rg.GET("/audit-logs", h.List)
}

// buildQuery assembles the repository filter from the request. Visible for
// tests. componentScope=false (platform admin) keeps the query platform-wide
// unless the caller narrows it; componentScope=true (component admin, decided
// by RequireAuditAccess) FORCES the component filter — a hand-tampered
// componentId cannot widen the view beyond the component the middleware
// already approved.
func buildQuery(c *gin.Context, componentScope bool) (repository.AuditQuery, error) {
	q := repository.AuditQuery{
		Subject:        c.Query("subject"),
		ActionPrefix:   c.Query("actionPrefix"),
		ResourceType:   c.Query("resourceType"),
		ResourceID:     c.Query("resourceId"),
		Ticket:         c.Query("ticket"),
		ReasonContains: c.Query("reason"),
	}
	if v := c.Query("componentId"); v != "" {
		cid, err := uuid.Parse(v)
		if err != nil {
			return q, fmt.Errorf("componentId 必须是 UUID")
		}
		q.ComponentID = &cid
	}
	if componentScope {
		if q.ComponentID == nil {
			return q, fmt.Errorf("组件级审计须携带 componentId 查询参数")
		}
	}
	return q, nil
}

// List returns audit rows newest-first. Filters (all optional):
//
//	componentId   — denormalized component attribution (组件级审计下沉；组件
//	                管理员被中间件+此 handler 强制钳到所辖组件)
//	subject       — exact subject (token sub or /group)
//	actionPrefix  — action prefix match, e.g. approval. → approval.approve / approval.reject
//	resourceType  — exact resource type
//	resourceId    — exact resource id
//	since/until   — RFC3339 time window (inclusive)
//	limit         — 1..500, default 100 (clamped in the repository too)
//	ticket        — exact change-ticket match (ui-12 §4.4)
//	reason        — substring match over the structured reason (ui-12 §4.4)
func (h *AuditLogHandler) List(c *gin.Context) {
	q, err := buildQuery(c, !middleware.AuditComponentScope(c))
	if err != nil {
		common.Fail(c, http.StatusBadRequest, err)
		return
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
