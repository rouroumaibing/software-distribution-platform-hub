package middleware

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/rouroumaibing/software-distribution-platform-hub/internal/common"
)

// These tests pin ACCOUNT-PERMISSION-MODEL §4 step 3 (鉴权 a：资源归属) as it
// is actually wired: orgs come from the token's `/org:<slug>` carrier → orgs.id,
// the check runs against the component that owns the route's resource, and a
// failure of any lookup is a 500 rather than a denial.

// fakeOwnershipChecker records what the gate asked about.
type fakeOwnershipChecker struct {
	gotComponentID uuid.UUID
	gotOrgIDs      []uuid.UUID
	calls          int
	allowed        bool
	err            error
}

func (f *fakeOwnershipChecker) AllowsComponent(componentID uuid.UUID, orgIDs []uuid.UUID) (bool, error) {
	f.calls++
	f.gotComponentID = componentID
	f.gotOrgIDs = orgIDs
	return f.allowed, f.err
}

// fakeOrgResolver stands in for the alias -> orgs.id join.
type fakeOrgResolver struct {
	bySlug map[string]uuid.UUID
	err    error
	got    []string
	calls  int
}

func (r *fakeOrgResolver) IDsBySlugs(slugs []string) (map[string]uuid.UUID, error) {
	r.calls++
	r.got = slugs
	return r.bySlug, r.err
}

// exerciseOwnership mounts the gate on a throwaway router. ctxOrgs == nil
// means "UserContext did not put an org dimension on the context".
func exerciseOwnership(t *testing.T, req Requirement, loc Locator, checker OwnershipChecker, orgs OrgAliasResolver, ctxOrgs []string, pathValue string) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	identity := func(c *gin.Context) {
		if ctxOrgs != nil {
			c.Set(contextKeyOrgs, ctxOrgs)
		}
		c.Next()
	}
	handler := func(c *gin.Context) { c.Status(http.StatusOK) }
	r.GET("/t/:"+req.Param, identity, RequireResourceOwnership(checker, orgs, loc, req), handler)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/t/"+pathValue, nil))
	return w
}

func TestRequireResourceOwnership_ResolvesAliasesAndDropsUnknownOnes(t *testing.T) {
	componentID, orgA := uuid.New(), uuid.New()
	checker := &fakeOwnershipChecker{allowed: true}
	resolver := &fakeOrgResolver{bySlug: map[string]uuid.UUID{"team-a": orgA}}

	w := exerciseOwnership(t,
		Requirement{Resource: ResourceComponent, Param: "id"},
		&fakeLocator{}, checker, resolver,
		[]string{"team-a", "ghost"}, componentID.String())

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if resolver.calls != 1 {
		t.Errorf("resolver called %d times, want exactly 1 (one query for all aliases)", resolver.calls)
	}
	if len(checker.gotOrgIDs) != 1 || checker.gotOrgIDs[0] != orgA {
		t.Errorf("checker got %v, want only the resolvable org %s (an unknown alias must not widen access)",
			checker.gotOrgIDs, orgA)
	}
	if checker.gotComponentID != componentID {
		t.Errorf("checked component %s, want %s", checker.gotComponentID, componentID)
	}
}

func TestRequireResourceOwnership_NoOrgsOnContextPassesEmpty(t *testing.T) {
	checker := &fakeOwnershipChecker{allowed: true}
	resolver := &fakeOrgResolver{bySlug: map[string]uuid.UUID{"team-a": uuid.New()}}

	w := exerciseOwnership(t,
		Requirement{Resource: ResourceComponent, Param: "id"},
		&fakeLocator{}, checker, resolver, nil, uuid.New().String())

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if len(checker.gotOrgIDs) != 0 {
		t.Errorf("checker got %v, want none", checker.gotOrgIDs)
	}
	if resolver.calls != 0 {
		t.Error("with no org aliases there is nothing to resolve — the resolver must not be queried")
	}
}

func TestRequireResourceOwnership_NilResolverIsNotAnError(t *testing.T) {
	// Partial wiring (no resolver handed in) must read as "no org dimension",
	// not blow up as a 500 on every request.
	checker := &fakeOwnershipChecker{allowed: true}

	w := exerciseOwnership(t,
		Requirement{Resource: ResourceComponent, Param: "id"},
		&fakeLocator{}, checker, nil, []string{"team-a"}, uuid.New().String())

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if len(checker.gotOrgIDs) != 0 {
		t.Errorf("checker got %v, want none", checker.gotOrgIDs)
	}
}

func TestRequireResourceOwnership_PipelineRouteUsesOwningComponent(t *testing.T) {
	pipelineID, componentID := uuid.New(), uuid.New()
	checker := &fakeOwnershipChecker{allowed: true}
	loc := &fakeLocator{componentID: componentID}

	w := exerciseOwnership(t,
		Requirement{Resource: ResourcePipeline, Param: "id"},
		loc, checker, nil, nil, pipelineID.String())

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if loc.gotPipeline != pipelineID {
		t.Errorf("locator saw %s, want %s", loc.gotPipeline, pipelineID)
	}
	if checker.gotComponentID != componentID {
		t.Errorf("ownership checked against %s, want the owning component %s — pipelines inherit their component's ownership",
			checker.gotComponentID, componentID)
	}
}

func TestRequireResourceOwnership_RunRouteUsesOwningComponent(t *testing.T) {
	runID, componentID := uuid.New(), uuid.New()
	checker := &fakeOwnershipChecker{allowed: true}
	loc := &fakeLocator{componentID: componentID}

	w := exerciseOwnership(t,
		Requirement{Resource: ResourceRun, Param: "id"},
		loc, checker, nil, nil, runID.String())

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if loc.gotRun != runID {
		t.Errorf("locator saw %s, want %s", loc.gotRun, runID)
	}
	if checker.gotComponentID != componentID {
		t.Errorf("ownership checked against %s, want %s", checker.gotComponentID, componentID)
	}
}

func TestRequireResourceOwnership_DenialIs403AndSaysWhy(t *testing.T) {
	w := exerciseOwnership(t,
		Requirement{Resource: ResourceComponent, Param: "id"},
		&fakeLocator{}, &fakeOwnershipChecker{allowed: false}, nil, nil, uuid.New().String())

	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", w.Code)
	}
	// The message must point at ownership, not at roles: the two 403s need
	// different fixes and a shared string sends people to the wrong table.
	if body := w.Body.String(); !strings.Contains(body, "not owned by any") {
		t.Errorf("403 body = %q, want it to name the ownership cause", body)
	}
}

func TestRequireResourceOwnership_CheckerFailureIs500(t *testing.T) {
	checker := &fakeOwnershipChecker{err: errors.New("ownership store down")}

	w := exerciseOwnership(t,
		Requirement{Resource: ResourceComponent, Param: "id"},
		&fakeLocator{}, checker, nil, nil, uuid.New().String())

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500 (an outage is not a denial)", w.Code)
	}
}

func TestRequireResourceOwnership_ResolverFailureIs500(t *testing.T) {
	resolver := &fakeOrgResolver{err: errors.New("orgs table down")}
	checker := &fakeOwnershipChecker{allowed: true}

	w := exerciseOwnership(t,
		Requirement{Resource: ResourceComponent, Param: "id"},
		&fakeLocator{}, checker, resolver, []string{"team-a"}, uuid.New().String())

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", w.Code)
	}
	if checker.calls != 0 {
		t.Error("the ownership check must not run when the org lookup failed")
	}
}

func TestRequireResourceOwnership_MissingResourceIs404(t *testing.T) {
	checker := &fakeOwnershipChecker{allowed: true}
	loc := &fakeLocator{err: common.ErrResourceNotFound}

	w := exerciseOwnership(t,
		Requirement{Resource: ResourcePipeline, Param: "id"},
		loc, checker, nil, nil, uuid.New().String())

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", w.Code)
	}
	if checker.calls != 0 {
		t.Error("the ownership check must not run when the resource does not exist")
	}
}

func TestRequireResourceOwnership_NonUUIDParamIs400(t *testing.T) {
	w := exerciseOwnership(t,
		Requirement{Resource: ResourceComponent, Param: "id"},
		&fakeLocator{}, &fakeOwnershipChecker{allowed: true}, nil, nil, "not-a-uuid")

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
}

func TestRequireResourceOwnership_PipelineRouteWithoutLocatorIs500(t *testing.T) {
	w := exerciseOwnership(t,
		Requirement{Resource: ResourcePipeline, Param: "id"},
		nil, &fakeOwnershipChecker{allowed: true}, nil, nil, uuid.New().String())

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500 (fail loudly on a wiring gap)", w.Code)
	}
}
