package service

import (
	"errors"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/rouroumaibing/software-distribution-platform-hub/internal/permission/models"
)

// ResourceTypeComponent is the only resource type the authorization chain
// consults. Ownership is registered per component; pipelines and runs inherit
// the ownership of the component that owns them, because every component
// scoped route resolves its path id to a component before checking anything
// (see middleware.RequireResourceOwnership). Keeping one granularity means an
// operator registers an owner once, not once per pipeline.
const ResourceTypeComponent = "component"

// OwnershipStore is the slice of the ownership repository this service needs.
// Narrowing to an interface follows the repo convention (ComponentStore,
// PlatformBindingStore, …) and lets the ownership *decision* — the one that
// can deny a request — be exercised without Postgres.
type OwnershipStore interface {
	Get(resourceType string, resourceID uuid.UUID) (*models.ResourceOwnership, error)
	Upsert(o *models.ResourceOwnership) error
	Delete(id uuid.UUID) error
}

// ResourceOwnershipService is the read/write front for the authoritative
// ownership table (ACCOUNT-PERMISSION-MODEL §3). The auth middleware's coarse
// ownership check (鉴权 a, §4 step 3) calls AllowsComponent; the org-id column
// denormalized onto component_role_bindings is now a derived cache rebuilt
// from this source.
type ResourceOwnershipService struct {
	repo OwnershipStore
}

func NewResourceOwnershipService(repo OwnershipStore) *ResourceOwnershipService {
	return &ResourceOwnershipService{repo: repo}
}

// Set upserts the ownership row for a (type, id), replacing any prior owner
// and share set.
func (s *ResourceOwnershipService) Set(resourceType string, resourceID, ownerOrg uuid.UUID, allowedOrgs []uuid.UUID) (*models.ResourceOwnership, error) {
	o := &models.ResourceOwnership{
		ResourceType: resourceType,
		ResourceID:   resourceID,
		OwnerOrg:     ownerOrg,
	}
	if err := o.SetAllowedOrgs(allowedOrgs); err != nil {
		return nil, err
	}
	if err := s.repo.Upsert(o); err != nil {
		return nil, err
	}
	return o, nil
}

func (s *ResourceOwnershipService) Get(resourceType string, resourceID uuid.UUID) (*models.ResourceOwnership, error) {
	return s.repo.Get(resourceType, resourceID)
}

// IsAllowed reports whether org may access the resource. Missing ownership row
// means "not owned here" → false (caller decides default), never a panic.
func (s *ResourceOwnershipService) IsAllowed(resourceType string, resourceID, org uuid.UUID) (bool, error) {
	o, err := s.repo.Get(resourceType, resourceID)
	if err != nil {
		return false, err
	}
	return o.IsAllowed(org), nil
}

// AllowsComponent answers the coarse half of authorization (鉴权 a, §4 step 3)
// for one component: may a request whose subject carries orgIDs touch this
// component? It implements the §3 decision formula
// `token.组织 ∈ {owner_org} ∪ allowed_orgs` exactly, including the cases the
// formula leaves implicit:
//
//	no ownership row                     → true   (归属未登记：本段无输入，
//	                                       放行交给 b 段 RBAC 细判)
//	row + orgIDs ∩ {owner, allowed} ≠ ∅  → true
//	row + orgIDs 非空但不相交             → false
//	row + orgIDs 为空                    → false  (fail-closed：资源已被显式
//	                                       声明归属，而请求证明不了任何归属)
//
// The last two lines are the deliberate asymmetry: an *unregistered* resource
// is unrestricted (today every resource is), but a *registered* one is
// enforced strictly. Any other split would leave a window in which an
// operator declares "component X belongs to org A" and requests from outside
// any org still reach the fine-grained stage.
//
// An empty orgIDs meaning "no org dimension on this request" is why the
// carrier being unpopulated (the current realm has no groups) does not lock
// anyone out: with no ownership rows the top row of the table applies.
//
// A lookup failure is returned as an error, never folded into false — an
// outage must stay distinguishable from a denial (same rule as
// RequirePermission's 500).
func (s *ResourceOwnershipService) AllowsComponent(componentID uuid.UUID, orgIDs []uuid.UUID) (bool, error) {
	row, err := s.repo.Get(ResourceTypeComponent, componentID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	for _, id := range orgIDs {
		if row.IsAllowed(id) {
			return true, nil
		}
	}
	return false, nil
}

func (s *ResourceOwnershipService) Delete(resourceType string, resourceID uuid.UUID) error {
	o, err := s.repo.Get(resourceType, resourceID)
	if err != nil {
		return err
	}
	return s.repo.Delete(o.ID)
}
