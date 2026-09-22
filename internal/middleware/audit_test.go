package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/rouroumaibing/software-distribution-platform-hub/internal/permission/models"
)

type captureSink struct{ entries []models.AuditLog }

func (s *captureSink) Record(e models.AuditLog) { s.entries = append(s.entries, e) }

func TestIsMutating(t *testing.T) {
	for _, m := range []string{http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete} {
		if !isMutating(m) {
			t.Errorf("isMutating(%s) = false, want true", m)
		}
	}
	for _, m := range []string{http.MethodGet, http.MethodHead} {
		if isMutating(m) {
			t.Errorf("isMutating(%s) = true, want false", m)
		}
	}
}

func TestDeriveAction(t *testing.T) {
	cases := []struct {
		method, path, want string
	}{
		{"POST", "/api/v1/pipelines", "pipelines:create"},
		{"PUT", "/api/v1/platform-roles/:id", "platform-roles:update"},
		{"DELETE", "/api/v1/pipelines/:id/runs", "pipelines:delete"},
		{"PATCH", "/api/v1/components/:id", "components:update"},
		{"POST", "/api/v1/", "unknown:create"},
	}
	for _, tc := range cases {
		if got := deriveAction(tc.method, tc.path); got != tc.want {
			t.Errorf("deriveAction(%s,%s) = %q, want %q", tc.method, tc.path, got, tc.want)
		}
	}
}

// The audit middleware must record mutating requests (with the resolved
// subject and the resulting status) and ignore reads — §6's "write ops only".
func TestAuditMiddleware_RecordsWritesOnly(t *testing.T) {
	gin.SetMode(gin.TestMode)
	sink := &captureSink{}
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set(contextKeySubject, "sub-1")
		c.Set(contextKeyPreferredUsername, "alice")
		c.Next()
	})
	r.Use(AuditMiddleware(sink))

	r.GET("/api/v1/things", func(c *gin.Context) { c.Status(http.StatusOK) })
	r.POST("/api/v1/things", func(c *gin.Context) { c.Status(http.StatusCreated) })
	r.DELETE("/api/v1/things/:id", func(c *gin.Context) { c.Status(http.StatusForbidden) })

	do := func(method, path string) {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(method, path, nil))
	}

	do(http.MethodGet, "/api/v1/things")
	if len(sink.entries) != 0 {
		t.Fatalf("GET must not be audited, got %d entries", len(sink.entries))
	}

	do(http.MethodPost, "/api/v1/things")
	if len(sink.entries) != 1 {
		t.Fatalf("POST should produce one audit entry, got %d", len(sink.entries))
	}
	e := sink.entries[0]
	if e.Action != "things:create" || e.Result != "success" || e.Subject != "sub-1" {
		t.Errorf("unexpected POST audit entry: %+v", e)
	}
	if e.StatusCode != http.StatusCreated {
		t.Errorf("status = %d, want 201", e.StatusCode)
	}

	do(http.MethodDelete, "/api/v1/things/abc")
	if len(sink.entries) != 2 {
		t.Fatalf("DELETE should produce a second audit entry, got %d", len(sink.entries))
	}
	d := sink.entries[1]
	if d.Action != "things:delete" || d.Result != "failure" || d.StatusCode != http.StatusForbidden {
		t.Errorf("a 403 write must be recorded as failure: %+v", d)
	}
	if d.ResourceID != "abc" {
		t.Errorf("resource id = %q, want abc", d.ResourceID)
	}
}

// A nil sink must not panic — the middleware is mounted unconditionally.
func TestAuditMiddleware_NilSinkIsSafe(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(AuditMiddleware(nil))
	r.POST("/api/v1/things", func(c *gin.Context) { c.Status(http.StatusOK) })
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/v1/things", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
}
