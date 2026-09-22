package service

import (
	"errors"
	"testing"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/rouroumaibing/software-distribution-platform-hub/internal/permission/models"
)

// fakeOwnershipStore is the DB-free stand-in for the ownership repository.
type fakeOwnershipStore struct {
	rows   map[string]*models.ResourceOwnership
	getErr error
}

func ownershipKey(resourceType string, id uuid.UUID) string {
	return resourceType + "/" + id.String()
}

func (f *fakeOwnershipStore) Get(resourceType string, id uuid.UUID) (*models.ResourceOwnership, error) {
	if f.getErr != nil {
		return nil, f.getErr
	}
	row, ok := f.rows[ownershipKey(resourceType, id)]
	if !ok {
		return nil, gorm.ErrRecordNotFound
	}
	return row, nil
}

func (f *fakeOwnershipStore) Upsert(o *models.ResourceOwnership) error {
	if f.rows == nil {
		f.rows = map[string]*models.ResourceOwnership{}
	}
	f.rows[ownershipKey(o.ResourceType, o.ResourceID)] = o
	return nil
}

func (f *fakeOwnershipStore) Delete(uuid.UUID) error { return nil }

// These tests pin ACCOUNT-PERMISSION-MODEL §3 (§4 step 3, 鉴权 a：资源归属).
// The decision table is the whole point of the method, so every row of it is
// asserted — including the two the formula leaves implicit (unregistered
// resource, and a registered resource reached with no org at all).

func newOwnershipSvc(t *testing.T, rows ...*models.ResourceOwnership) (*ResourceOwnershipService, uuid.UUID, uuid.UUID) {
	t.Helper()
	owner, other := uuid.New(), uuid.New()
	store := &fakeOwnershipStore{rows: map[string]*models.ResourceOwnership{}}
	for _, r := range rows {
		store.rows[ownershipKey(r.ResourceType, r.ResourceID)] = r
	}
	return NewResourceOwnershipService(store), owner, other
}

func TestAllowsComponent_UnregisteredResourceIsUnrestricted(t *testing.T) {
	svc, _, _ := newOwnershipSvc(t)
	componentID := uuid.New()

	// Both with and without orgs: an unregistered resource gives this stage no
	// input at all, so it must not deny. (Today *every* resource is here, which
	// is what keeps the new gate from locking the platform out.)
	for _, orgs := range [][]uuid.UUID{nil, {uuid.New()}} {
		allowed, err := svc.AllowsComponent(componentID, orgs)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !allowed {
			t.Errorf("unregistered component denied with orgs=%v — 归属未登记不该拦", orgs)
		}
	}
}

func TestAllowsComponent_OwnerOrgIsAllowed(t *testing.T) {
	owner := uuid.New()
	componentID := uuid.New()
	svc, _, _ := newOwnershipSvc(t, &models.ResourceOwnership{
		ResourceType: ResourceTypeComponent, ResourceID: componentID, OwnerOrg: owner,
	})

	allowed, err := svc.AllowsComponent(componentID, []uuid.UUID{owner})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !allowed {
		t.Error("the owning org must be allowed (§3: token.组织 ∈ {owner_org} ∪ allowed_orgs)")
	}
}

func TestAllowsComponent_SharedOrgIsAllowed(t *testing.T) {
	owner, sibling := uuid.New(), uuid.New()
	componentID := uuid.New()
	row := &models.ResourceOwnership{
		ResourceType: ResourceTypeComponent, ResourceID: componentID, OwnerOrg: owner,
	}
	if err := row.SetAllowedOrgs([]uuid.UUID{sibling}); err != nil {
		t.Fatalf("SetAllowedOrgs: %v", err)
	}
	svc, _, _ := newOwnershipSvc(t, row)

	allowed, err := svc.AllowsComponent(componentID, []uuid.UUID{sibling})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !allowed {
		t.Error("an org in the share set must be allowed")
	}
}

func TestAllowsComponent_OtherOrgIsDenied(t *testing.T) {
	owner := uuid.New()
	componentID := uuid.New()
	svc, _, other := newOwnershipSvc(t, &models.ResourceOwnership{
		ResourceType: ResourceTypeComponent, ResourceID: componentID, OwnerOrg: owner,
	})

	allowed, err := svc.AllowsComponent(componentID, []uuid.UUID{other})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if allowed {
		t.Error("an unrelated org must be denied")
	}
}

// The asymmetry that matters: once a resource IS registered, an org-less
// request is denied rather than waved through. Otherwise declaring ownership
// would open a window where requests carrying no org still reach the
// fine-grained stage.
func TestAllowsComponent_RegisteredResourceWithNoOrgsIsDenied(t *testing.T) {
	componentID := uuid.New()
	svc, _, _ := newOwnershipSvc(t, &models.ResourceOwnership{
		ResourceType: ResourceTypeComponent, ResourceID: componentID, OwnerOrg: uuid.New(),
	})

	allowed, err := svc.AllowsComponent(componentID, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if allowed {
		t.Error("a registered resource reached with no org must fail closed")
	}
}

func TestAllowsComponent_UUIDNilNeverMatches(t *testing.T) {
	// A mis-resolved alias must not become uuid.Nil and then match everything.
	componentID := uuid.New()
	svc, _, _ := newOwnershipSvc(t, &models.ResourceOwnership{
		ResourceType: ResourceTypeComponent, ResourceID: componentID, OwnerOrg: uuid.New(),
	})

	allowed, err := svc.AllowsComponent(componentID, []uuid.UUID{uuid.Nil})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if allowed {
		t.Error("uuid.Nil must never satisfy an ownership check")
	}
}

func TestAllowsComponent_OnlyComponentRowsAreConsulted(t *testing.T) {
	// The gate always asks about ResourceTypeComponent. A row registered under
	// another resource type must not accidentally answer for a component.
	componentID := uuid.New()
	svc, _, _ := newOwnershipSvc(t, &models.ResourceOwnership{
		ResourceType: "pipeline", ResourceID: componentID, OwnerOrg: uuid.New(),
	})

	allowed, err := svc.AllowsComponent(componentID, []uuid.UUID{uuid.New()})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !allowed {
		t.Error("a pipeline-typed row must not be read as this component's ownership")
	}
}

func TestAllowsComponent_StoreFailureIsAnErrorNotADenial(t *testing.T) {
	svc := NewResourceOwnershipService(&fakeOwnershipStore{getErr: errors.New("db down")})

	allowed, err := svc.AllowsComponent(uuid.New(), nil)
	if err == nil {
		t.Fatal("a store crash must surface as an error so the middleware can answer 500")
	}
	if allowed {
		t.Error("a failed lookup must not report allowed")
	}
}
