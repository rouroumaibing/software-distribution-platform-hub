package middleware

import (
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/rouroumaibing/software-distribution-platform-hub/internal/permission/models"
)

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
func AuditMiddleware(sink AuditSink) gin.HandlerFunc {
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
			Result:       result,
			StatusCode:   c.Writer.Status(),
		})
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
