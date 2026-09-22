package service

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/google/uuid"

	"github.com/rouroumaibing/software-distribution-platform-hub/internal/common"
	"github.com/rouroumaibing/software-distribution-platform-hub/internal/permission/models"
)

// ComponentRoleStore is the narrow persistence surface the service needs.
// *repository.ComponentRoleRepository satisfies it, and so can a stub — which
// keeps every validation branch unit-testable without Postgres (the same
// pattern PlatformRoleStore uses).
type ComponentRoleStore interface {
	List() ([]models.ComponentRole, error)
	GetByID(id uuid.UUID) (*models.ComponentRole, error)
	GetByNameInOrg(orgID *uuid.UUID, name string) (*models.ComponentRole, error)
	Create(item *models.ComponentRole) error
	Update(item *models.ComponentRole) error
	Delete(id uuid.UUID) error
	CountBindings(roleID uuid.UUID) (int64, error)
}

// ComponentRoleService owns §7.3 component-scoped role definitions (B-11
// 自定义角色). Built-ins (org_id IS NULL, `IsSystem`) are seeded and stay
// immutable; org-scoped custom roles are created here.
//
// Error seq values start at 20 so the component-role family never collides
// with the platform-role family's 1–4 in the ERR.<mod><http><seq> scheme —
// two different conditions sharing one ErrorCode would make the code useless
// for support ("which 409 is this?").
type ComponentRoleService struct{ store ComponentRoleStore }

func NewComponentRoleService(store ComponentRoleStore) *ComponentRoleService {
	return &ComponentRoleService{store: store}
}

func errComponentRoleNameRequired() *common.APIError {
	return common.DomainError(common.KindPermission, http.StatusBadRequest, 20, "name is required")
}

func errComponentRoleActionsRequired() *common.APIError {
	return common.DomainError(common.KindPermission, http.StatusBadRequest, 21, "actions must be a non-empty JSON array of strings")
}

func errComponentRoleExists() *common.APIError {
	return common.DomainError(common.KindPermission, http.StatusConflict, 22, "Component Role Name Already Exists In This Org")
}

func errComponentRoleNotFound() *common.APIError {
	return common.DomainError(common.KindPermission, http.StatusNotFound, 23, "Component Role Not Found")
}

func errComponentRoleImmutable() *common.APIError {
	return common.DomainError(common.KindPermission, http.StatusConflict, 24, "Built-in Component Role Is Immutable")
}

func errComponentRoleInUse(n int64) *common.APIError {
	return common.DomainErrorWithReasons(
		common.KindPermission, http.StatusConflict, 25,
		"Component Role Is In Use",
		"仍有 "+strconv.FormatInt(n, 10)+" 条组件级绑定引用该角色，请先删除绑定",
	)
}

// List returns every component role (built-in + custom, all orgs).
func (s *ComponentRoleService) List() ([]models.ComponentRole, error) { return s.store.List() }

func (s *ComponentRoleService) Get(id uuid.UUID) (*models.ComponentRole, error) {
	item, err := s.store.GetByID(id)
	if err != nil {
		return nil, errComponentRoleNotFound()
	}
	return item, nil
}

// Create validates and inserts a custom component role.
//
// A role that grants nothing is rejected: it silently authorizes nobody, and
// the mistake only surfaces much later as a confusing 403.
func (s *ComponentRoleService) Create(in *models.ComponentRole) error {
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" {
		return errComponentRoleNameRequired()
	}
	if !validActions(in.Actions) {
		return errComponentRoleActionsRequired()
	}
	if existing, err := s.store.GetByNameInOrg(in.OrgID, in.Name); err == nil && existing != nil {
		// Only a hit is meaningful; not-found is the happy path and is
		// deliberately not surfaced (a genuine persistence failure is
		// reported by the insert itself).
		return errComponentRoleExists()
	}
	// Client-supplied identity is ignored: id / isSystem / createdAt are
	// server-owned, so a caller cannot forge a built-in (immutable) role.
	in.IsSystem = false
	return s.store.Create(in)
}

// Update rewrites description/actions of a custom role. The org scope is
// deliberately NOT updatable: moving a role between orgs would silently
// change which bindings it authorizes. Built-ins are immutable so a console
// mistake cannot strip `component-admin` of its actions.
func (s *ComponentRoleService) Update(id uuid.UUID, in *models.ComponentRole) error {
	cur, err := s.store.GetByID(id)
	if err != nil {
		return errComponentRoleNotFound()
	}
	if cur.IsSystem {
		return errComponentRoleImmutable()
	}
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" {
		return errComponentRoleNameRequired()
	}
	if !validActions(in.Actions) {
		return errComponentRoleActionsRequired()
	}
	// Duplicate check stays inside the role's own org, and must not match
	// the row being edited.
	if other, gerr := s.store.GetByNameInOrg(cur.OrgID, in.Name); gerr == nil && other != nil && other.ID != id {
		return errComponentRoleExists()
	}
	// Read-modify-write: Save persists every mapped column, so carry the
	// server-owned fields forward instead of letting them fall back to zero.
	cur.Name = in.Name
	cur.Description = in.Description
	cur.Actions = in.Actions
	return s.store.Update(cur)
}

// Delete removes a custom role, refusing while bindings still reference it
// (409 + reasons) — refuse-rather-than-orphan, same rule as the platform-role
// service and the hub's DELETE contract.
func (s *ComponentRoleService) Delete(id uuid.UUID) error {
	cur, err := s.store.GetByID(id)
	if err != nil {
		return errComponentRoleNotFound()
	}
	if cur.IsSystem {
		return errComponentRoleImmutable()
	}
	n, cerr := s.store.CountBindings(id)
	if cerr != nil {
		return cerr
	}
	if n > 0 {
		return errComponentRoleInUse(n)
	}
	return s.store.Delete(id)
}
