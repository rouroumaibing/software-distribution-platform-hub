package middleware

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/rouroumaibing/software-distribution-platform-hub/internal/common"
	"github.com/rouroumaibing/software-distribution-platform-hub/internal/permission/models"
)

// Context keys for structured high-risk-operation context (ui-12 §4.4, N-22).
// Handlers call SetAuditReason during request processing; AuditMiddleware
// picks the values up after c.Next() and stores them on the audit row.
const (
	auditReasonKey = "sdp.audit.reason"
	auditTicketKey = "sdp.audit.ticket"
)

// SetAuditReason attaches the structured reason / change-ticket to the
// request's audit entry. Safe to call on any request; values are only
// persisted for mutating requests that reach the audit sink.
func SetAuditReason(c *gin.Context, reason, ticket string) {
	if reason != "" {
		c.Set(auditReasonKey, reason)
	}
	if ticket != "" {
		c.Set(auditTicketKey, ticket)
	}
}

// AuditSink persists audit entries. It is deliberately narrow so the middleware
// doesn't import the whole permission service graph. *permissionsvc.AuditReporter
// satisfies it.
type AuditSink interface {
	Record(entry models.AuditLog)
}

// AuditMiddleware records every mutating request (POST/PUT/PATCH/DELETE) into
// the audit log. Mount it after UserContext so the subject is resolved, and
// register it before the routes so it wraps every handler (gin runs the
// post-`c.Next()` part after the handler returns, which is where the status
// code and result are known). Per ACCOUNT-PERMISSION-MODEL §6, business code
// never writes audit rows itself — this is the single, cross-cutting writer.
//
// loc is optional: when wired, each mutating row is denormalized with the
// owning component id (ui-12 5.4 组件级审计下沉) — "components" resources are
// their own owner; "pipelines"/"runs" resolve through the same Locator the
// RBAC route guards use. Unresolvable rows simply stay unattributed.
func AuditMiddleware(sink AuditSink, loc Locator) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Next()

		if !isMutating(c.Request.Method) {
			return
		}
		if sink == nil {
			return
		}

		subject, _ := CurrentSubject(c)
		result := "success"
		if c.Writer.Status() >= http.StatusBadRequest {
			result = "failure"
		}
		sink.Record(models.AuditLog{
			Timestamp:    time.Now(),
			SubjectType:  "user",
			Subject:      subject,
			Roles:        CurrentPreferredUsername(c),
			SourceIP:     c.ClientIP(),
			Action:       deriveAction(c.Request.Method, c.FullPath()),
			ResourceType: resourceTypeOf(c.FullPath()),
			ResourceID:   c.Param("id"),
			ComponentID:  auditComponentOf(loc, resourceTypeOf(c.FullPath()), c.Param("id")),
			Result:       result,
			StatusCode:   c.Writer.Status(),
			Reason:       c.GetString(auditReasonKey),
			Ticket:       c.GetString(auditTicketKey),
		})
	}
}

// auditComponentOf maps an audit row's (resourceType, resourceID) to the
// owning component, best-effort: unknown resource types, non-UUID ids and
// locator failures all yield nil (row stays unattributed — attribution feeds
// the component-scope audit view, not any gate).
func auditComponentOf(loc Locator, resourceType, resourceID string) *uuid.UUID {
	if loc == nil || resourceID == "" {
		return nil
	}
	id, err := uuid.Parse(resourceID)
	if err != nil {
		return nil
	}
	var (
		componentID uuid.UUID
		err2        error
	)
	switch resourceType {
	case "components":
		componentID = id
	case "pipelines":
		componentID, err2 = loc.ComponentOfPipeline(id)
	case "runs":
		componentID, err2 = loc.ComponentOfRun(id)
	default:
		return nil
	}
	if err2 != nil || componentID == uuid.Nil {
		return nil
	}
	return &componentID
}

// auditPlatformScopeKey marks "caller passed the platform-admin branch" on
// the gin context; the audit handler reads it to decide whether the query is
// platform-wide or forced onto the caller's component filter.
const auditPlatformScopeKey = "sdp.audit.platformScope"

// AuditComponentScope exposes the middleware's decision to the handler.
func AuditComponentScope(c *gin.Context) bool {
	return c.GetBool(auditPlatformScopeKey)
}

// RequireAuditAccess gates GET /audit-logs with the two-tier rule behind the
// 组件级审计下沉 (ui-12 5.4 拍板 2026-10-11):
//
//  1. platform user:manage → platform-wide access (previous behavior);
//  2. otherwise the request must carry ?componentId= and the subject must
//     hold component:manage on that component — the query is then forced
//     onto that component by the handler.
//
// Mounting note: this replaces the old "audit-logs only under platformGroup"
// discipline. The exposure increase is bounded by the handler's forced
// component filter — see audit.go 的鉴权约束注记.
func RequireAuditAccess(platformChecker PlatformPermissionChecker, checker PermissionChecker) gin.HandlerFunc {
	return func(c *gin.Context) {
		subject, ok := CurrentSubject(c)
		if !ok {
			common.Fail(c, http.StatusUnauthorized, errNoSubject)
			c.Abort()
			return
		}
		allowed, err := platformChecker.HasPlatformPermission(nil, subject, CurrentGroups(c), models.ActionUserManage)
		if err != nil {
			common.Fail(c, http.StatusInternalServerError, err)
			c.Abort()
			return
		}
		if allowed {
			c.Set(auditPlatformScopeKey, true)
			c.Next()
			return
		}
		raw := c.Query("componentId")
		if raw == "" {
			common.Fail(c, http.StatusForbidden, errors.New("组件级审计须携带 componentId 查询参数"))
			c.Abort()
			return
		}
		componentID, perr := uuid.Parse(raw)
		if perr != nil {
			common.Fail(c, http.StatusBadRequest, errors.New("componentId 必须是 UUID"))
			c.Abort()
			return
		}
		okComp, cerr := checker.HasPermission(componentID, subject, CurrentGroups(c), models.ActionComponentManage)
		if cerr != nil {
			// 与 RequirePermission 同口径：查询失败是基础设施问题，
			// 伪装成 403 会把故障藏进权限语义里。
			common.Fail(c, http.StatusInternalServerError, cerr)
			c.Abort()
			return
		}
		if !okComp {
			common.Fail(c, http.StatusForbidden, errForbidden)
			c.Abort()
			return
		}
		c.Next()
	}
}

func isMutating(method string) bool {
	switch method {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return true
	default:
		return false
	}
}

// deriveAction turns "POST /api/v1/pipelines/:id/runs" into "pipelines:create"
// — a coarse but stable audit action. The verb follows the HTTP method.
func deriveAction(method, fullPath string) string {
	segment := ""
	for _, s := range strings.Split(strings.TrimPrefix(fullPath, "/api/v1/"), "/") {
		if s == "" || strings.HasPrefix(s, ":") {
			continue
		}
		segment = s
		break
	}
	if segment == "" {
		segment = "unknown"
	}
	verb := map[string]string{
		http.MethodPost:   "create",
		http.MethodPut:    "update",
		http.MethodPatch:  "update",
		http.MethodDelete: "delete",
	}[method]
	return segment + ":" + verb
}

// resourceTypeOf extracts the leading resource segment (used as the audit's
// resource_type). Mirrors deriveAction's segment selection.
func resourceTypeOf(fullPath string) string {
	for _, s := range strings.Split(strings.TrimPrefix(fullPath, "/api/v1/"), "/") {
		if s == "" || strings.HasPrefix(s, ":") {
			continue
		}
		return s
	}
	return ""
}
