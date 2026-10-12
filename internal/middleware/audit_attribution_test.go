package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// ui-12 5.4 组件级审计下沉：审计行写时反规范化所属组件 + 读端点双通道鉴权。
// 复用 rbac_test.go 的 fakeLocator（单一 componentID + err 注入）。

func newAuditRouter(sink AuditSink, loc Locator) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set(contextKeySubject, "sub-1")
		c.Next()
	})
	r.Use(AuditMiddleware(sink, loc))
	r.POST("/api/v1/components/:id/environments", func(c *gin.Context) { c.Status(http.StatusCreated) })
	r.POST("/api/v1/pipelines/:id/runs", func(c *gin.Context) { c.Status(http.StatusCreated) })
	r.POST("/api/v1/runs/:id/cancel", func(c *gin.Context) { c.Status(http.StatusOK) })
	r.POST("/api/v1/things/:id", func(c *gin.Context) { c.Status(http.StatusOK) })
	return r
}

func TestAuditMiddleware_DenormalizesComponent(t *testing.T) {
	sink := &captureSink{}
	locatedComp := uuid.New()
	r := newAuditRouter(sink, &fakeLocator{componentID: locatedComp})

	do := func(path string) {
		r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, path, nil))
	}
	compID := uuid.New()
	do("/api/v1/components/" + compID.String() + "/environments")
	do("/api/v1/pipelines/11111111-1111-1111-1111-111111111111/runs")
	do("/api/v1/runs/22222222-2222-2222-2222-222222222222/cancel")
	do("/api/v1/things/33333333-3333-3333-3333-333333333333")
	do("/api/v1/components/not-a-uuid/environments")

	if len(sink.entries) != 5 {
		t.Fatalf("expected 5 audit entries, got %d", len(sink.entries))
	}
	// components 直取资源自身 id；pipelines/runs 经 Locator 回查 —— 两类
	// 来源必须能区分（components 行不等恒等于 Locator 结果）。
	if sink.entries[0].ComponentID == nil || *sink.entries[0].ComponentID != compID {
		t.Errorf("components 资源应直取自身为归属组件，得到 %v", sink.entries[0].ComponentID)
	}
	if sink.entries[1].ComponentID == nil || *sink.entries[1].ComponentID != locatedComp {
		t.Errorf("pipelines 资源应经 Locator 回查归属组件，得到 %v", sink.entries[1].ComponentID)
	}
	if sink.entries[2].ComponentID == nil || *sink.entries[2].ComponentID != locatedComp {
		t.Errorf("runs 资源应经 Locator 回查归属组件，得到 %v", sink.entries[2].ComponentID)
	}
	if sink.entries[3].ComponentID != nil {
		t.Errorf("未知资源类型不应归属，得到 %v", sink.entries[3].ComponentID)
	}
	if sink.entries[4].ComponentID != nil {
		t.Errorf("非 UUID 资源 id 不应归属，得到 %v", sink.entries[4].ComponentID)
	}
}

func TestAuditMiddleware_LocatorFailureStaysUnattributed(t *testing.T) {
	sink := &captureSink{}
	r := newAuditRouter(sink, &fakeLocator{err: errNoLocator})
	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/api/v1/runs/22222222-2222-2222-2222-222222222222/cancel", nil))
	if len(sink.entries) != 1 || sink.entries[0].ComponentID != nil {
		t.Fatalf("Locator 失败应留空归属（best-effort，不影响写行），得到 %+v", sink.entries)
	}
}

type fakeAuditPermChecker struct {
	platform bool
	compOK   bool
}

func (f fakeAuditPermChecker) HasPlatformPermission(*uuid.UUID, string, []string, string) (bool, error) {
	return f.platform, nil
}

func (f fakeAuditPermChecker) HasPermission(uuid.UUID, string, []string, string) (bool, error) {
	return f.compOK, nil
}

func newAuditAccessRouter(checker fakeAuditPermChecker) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set(contextKeySubject, "sub-1")
		c.Next()
	})
	r.Use(RequireAuditAccess(checker, checker))
	r.GET("/api/v1/audit-logs", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"platformScope": AuditComponentScope(c)})
	})
	return r
}

func TestRequireAuditAccess_PlatformAdminFullScope(t *testing.T) {
	r := newAuditAccessRouter(fakeAuditPermChecker{platform: true})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/audit-logs", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("平台管理员应放行，得到 %d", w.Code)
	}
	if w.Body.String() == "" || !contains(w.Body.String(), `"platformScope":true`) {
		t.Fatalf("平台管理员应标记 platformScope=true，得到 %s", w.Body.String())
	}
}

func TestRequireAuditAccess_ComponentAdminScoping(t *testing.T) {
	checker := fakeAuditPermChecker{platform: false, compOK: true}
	r := newAuditAccessRouter(checker)
	cid := uuid.New().String()

	// 携带 componentId + component:manage → 放行，platformScope=false。
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/audit-logs?componentId="+cid, nil))
	if w.Code != http.StatusOK {
		t.Fatalf("组件管理员应放行，得到 %d", w.Code)
	}
	if !contains(w.Body.String(), `"platformScope":false`) {
		t.Fatalf("组件管理员应标记 platformScope=false，得到 %s", w.Body.String())
	}

	// 不带 componentId → 403（组件级审计必须有组件边界）。
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, httptest.NewRequest(http.MethodGet, "/api/v1/audit-logs", nil))
	if w2.Code != http.StatusForbidden {
		t.Fatalf("缺 componentId 应 403，得到 %d", w2.Code)
	}

	// 非 UUID componentId → 400。
	w3 := httptest.NewRecorder()
	r.ServeHTTP(w3, httptest.NewRequest(http.MethodGet, "/api/v1/audit-logs?componentId=abc", nil))
	if w3.Code != http.StatusBadRequest {
		t.Fatalf("非法 componentId 应 400，得到 %d", w3.Code)
	}
}

func TestRequireAuditAccess_ComponentAdminDenied(t *testing.T) {
	r := newAuditAccessRouter(fakeAuditPermChecker{platform: false, compOK: false})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/audit-logs?componentId="+uuid.New().String(), nil))
	if w.Code != http.StatusForbidden {
		t.Fatalf("无 component:manage 应 403，得到 %d", w.Code)
	}
}

func contains(haystack, needle string) bool {
	return strings.Contains(haystack, needle)
}
