package middleware

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/rouroumaibing/software-distribution-platform-hub/internal/common"
)

// These tests pin ACCOUNT-PERMISSION-MODEL §10 #14: a route whose path id is
// a pipeline or run id must authorize against the *owning component*, not
// against the id sitting in the path. Before the fix those routes handed a
// pipeline id (or a run id) to the component-binding lookup, matched no
// binding, and denied every request with 403 the moment auth was enabled.

// fakeChecker records which component id the middleware asked about.
type fakeChecker struct {
	gotComponentID uuid.UUID
	gotSubject     string
	allowed        bool
	err            error
}

func (f *fakeChecker) HasPermission(componentID uuid.UUID, subject string, groups []string, permission string) (bool, error) {
	f.gotComponentID = componentID
	f.gotSubject = subject
	return f.allowed, f.err
}

// fakeLocator stands in for the pipeline/run -> component resolution.
type fakeLocator struct {
	componentID uuid.UUID
	err         error
	gotPipeline uuid.UUID
	gotRun      uuid.UUID
}

func (l *fakeLocator) ComponentOfPipeline(id uuid.UUID) (uuid.UUID, error) {
	l.gotPipeline = id
	return l.componentID, l.err
}

func (l *fakeLocator) ComponentOfRun(id uuid.UUID) (uuid.UUID, error) {
	l.gotRun = id
	return l.componentID, l.err
}

// exercise mounts req behind RequirePermission on a throwaway router and
// returns the response. A uuid.Nil subjectSeed means "nothing on context";
// otherwise a subject is stamped so the middleware has an identity to work
// with. The concrete value is irrelevant — these tests are about *which
// component* the binding is checked against, not about who is asking.
func exercise(t *testing.T, req Requirement, loc Locator, checker PermissionChecker, subjectSeed uuid.UUID, pathValue string) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	identity := func(c *gin.Context) {
		if subjectSeed != uuid.Nil {
			c.Set(contextKeySubject, "sub-"+subjectSeed.String())
		}
		c.Next()
	}
	handler := func(c *gin.Context) { c.Status(http.StatusOK) }
	r.GET("/t/:"+req.Param, identity, RequirePermission(checker, loc, req), handler)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/t/"+pathValue, nil))
	return w
}

func TestRequirePermission_ComponentRouteUsesPathIDAsComponentID(t *testing.T) {
	componentID := uuid.New()
	checker := &fakeChecker{allowed: true}
	loc := &fakeLocator{}

	w := exercise(t, Requirement{Resource: ResourceComponent, Param: "id", Permission: "component:read"},
		loc, checker, uuid.New(), componentID.String())

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if checker.gotComponentID != componentID {
		t.Errorf("binding checked against %s, want the path id %s", checker.gotComponentID, componentID)
	}
	if loc.gotPipeline != uuid.Nil || loc.gotRun != uuid.Nil {
		t.Error("a component-addressed route must not consult the locator")
	}
}

func TestRequirePermission_PipelineRouteChecksOwningComponent(t *testing.T) {
	pipelineID, componentID := uuid.New(), uuid.New()
	checker := &fakeChecker{allowed: true}
	loc := &fakeLocator{componentID: componentID}

	w := exercise(t, Requirement{Resource: ResourcePipeline, Param: "id", Permission: "pipeline:trigger"},
		loc, checker, uuid.New(), pipelineID.String())

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if loc.gotPipeline != pipelineID {
		t.Errorf("locator saw pipeline %s, want %s", loc.gotPipeline, pipelineID)
	}
	if checker.gotComponentID != componentID {
		t.Errorf("binding checked against %s, want the owning component %s", checker.gotComponentID, componentID)
	}
	if checker.gotComponentID == pipelineID {
		t.Error("binding was checked against the pipeline id itself — §10 #14 regression")
	}
}

func TestRequirePermission_RunRouteChecksOwningComponent(t *testing.T) {
	runID, componentID := uuid.New(), uuid.New()
	checker := &fakeChecker{allowed: true}
	loc := &fakeLocator{componentID: componentID}

	w := exercise(t, Requirement{Resource: ResourceRun, Param: "id", Permission: "pipeline:trigger"},
		loc, checker, uuid.New(), runID.String())

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if loc.gotRun != runID {
		t.Errorf("locator saw run %s, want %s", loc.gotRun, runID)
	}
	if checker.gotComponentID != componentID {
		t.Errorf("binding checked against %s, want the owning component %s", checker.gotComponentID, componentID)
	}
}

func TestRequirePermission_MissingResourceIs404(t *testing.T) {
	checker := &fakeChecker{allowed: true}
	loc := &fakeLocator{err: common.ErrResourceNotFound}

	w := exercise(t, Requirement{Resource: ResourceRun, Param: "id", Permission: "pipeline:trigger"},
		loc, checker, uuid.New(), uuid.New().String())

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", w.Code)
	}
	if checker.gotComponentID != uuid.Nil {
		t.Error("the binding lookup must not run when the resource does not exist")
	}
}

func TestRequirePermission_StoreFailureIs500(t *testing.T) {
	checker := &fakeChecker{allowed: true}
	loc := &fakeLocator{err: errors.New("db down")}

	w := exercise(t, Requirement{Resource: ResourcePipeline, Param: "id", Permission: "pipeline:trigger"},
		loc, checker, uuid.New(), uuid.New().String())

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500 (a lookup crash is not a 404)", w.Code)
	}
}

func TestRequirePermission_NonUUIDParamIs400(t *testing.T) {
	w := exercise(t, Requirement{Resource: ResourceComponent, Param: "id", Permission: "component:read"},
		&fakeLocator{}, &fakeChecker{allowed: true}, uuid.New(), "not-a-uuid")

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
}

func TestRequirePermission_NoIdentityIs401(t *testing.T) {
	w := exercise(t, Requirement{Resource: ResourceComponent, Param: "id", Permission: "component:read"},
		&fakeLocator{}, &fakeChecker{allowed: true}, uuid.Nil, uuid.New().String())

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", w.Code)
	}
}

func TestRequirePermission_DeniedIs403(t *testing.T) {
	w := exercise(t, Requirement{Resource: ResourceComponent, Param: "id", Permission: "component:read"},
		&fakeLocator{}, &fakeChecker{allowed: false}, uuid.New(), uuid.New().String())

	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", w.Code)
	}
}

func TestRequirePermission_CheckerFailureIs500(t *testing.T) {
	checker := &fakeChecker{err: errors.New("binding store down")}

	w := exercise(t, Requirement{Resource: ResourceComponent, Param: "id", Permission: "component:read"},
		&fakeLocator{}, checker, uuid.New(), uuid.New().String())

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500 (not 403 — an outage is not a denial)", w.Code)
	}
}

func TestRequirePermission_PipelineRouteWithoutLocatorIs500(t *testing.T) {
	// Wiring gap: somebody mounted a pipeline-addressed route but forgot the
	// locator. Fail loudly instead of silently denying.
	w := exercise(t, Requirement{Resource: ResourcePipeline, Param: "id", Permission: "pipeline:trigger"},
		nil, &fakeChecker{allowed: true}, uuid.New(), uuid.New().String())

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", w.Code)
	}
}
