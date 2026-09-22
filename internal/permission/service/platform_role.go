package service

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/google/uuid"

	"github.com/rouroumaibing/software-distribution-platform-hub/internal/common"
	"github.com/rouroumaibing/software-distribution-platform-hub/internal/permission/models"
)

// PlatformRoleStore is the narrow persistence surface the service needs.
// *repository.PlatformRoleRepository satisfies it, and so can a stub, which
// keeps every validation branch unit-testable without Postgres.
type PlatformRoleStore interface {
	List() ([]models.PlatformRole, error)
	GetByID(id uuid.UUID) (*models.PlatformRole, error)
	GetByName(name string) (*models.PlatformRole, error)
	Create(item *models.PlatformRole) error
	Update(item *models.PlatformRole) error
	Delete(id uuid.UUID) error
	CountBindings(id uuid.UUID) (int64, error)
}

// PlatformRoleService owns platform-level role definitions (§5.1 ①). Before
// C-10 these existed only as seed SQL; the API lets a platform admin define
// org-scoped custom roles.
//
// Errors are built by functions, never by the shared package-level *APIError
// singletons: those get rewritten in place by WithMessage/WithError, so
// reusing one across concurrent requests lets two responses overwrite each
// other's message.
type PlatformRoleService struct{ store PlatformRoleStore }

func NewPlatformRoleService(store PlatformRoleStore) *PlatformRoleService {
	return &PlatformRoleService{store: store}
}

func errPlatformRoleNameRequired() *common.APIError {
	return common.DomainError(common.KindPermission, http.StatusBadRequest, 10, "name is required")
}

func errPlatformRoleActionsRequired() *common.APIError {
	return common.DomainError(common.KindPermission, http.StatusBadRequest, 11, "actions must be a non-empty JSON array of strings")
}

func errPlatformRoleExists() *common.APIError {
	return common.DomainError(common.KindPermission, http.StatusConflict, 2, "Platform Role Name Already Exists")
}

func errPlatformRoleNotFound() *common.APIError {
	return common.DomainError(common.KindPermission, http.StatusNotFound, 1, "Platform Role Not Found")
}

func errPlatformRoleImmutable() *common.APIError {
	return common.DomainError(common.KindPermission, http.StatusConflict, 3, "Built-in Platform Role Is Immutable")
}

func errPlatformRoleInUse(n int64) *common.APIError {
	return common.DomainErrorWithReasons(
		common.KindPermission, http.StatusConflict, 4,
		"Platform Role Is In Use",
		"仍有 "+strconv.FormatInt(n, 10)+" 条平台级绑定引用该角色，请先删除绑定",
	)
}

// List returns every platform role (built-in + custom).
func (s *PlatformRoleService) List() ([]models.PlatformRole, error) { return s.store.List() }

// Get returns one role, mapping a miss to the coded 404 so the handler does
// not have to distinguish gorm.ErrRecordNotFound itself.
func (s *PlatformRoleService) Get(id uuid.UUID) (*models.PlatformRole, error) {
	item, err := s.store.GetByID(id)
	if err != nil {
		return nil, errPlatformRoleNotFound()
	}
	return item, nil
}

// Create validates and inserts a platform role.
func (s *PlatformRoleService) Create(in *models.PlatformRole) error {
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" {
		return errPlatformRoleNameRequired()
	}
	if !validActions(in.Actions) {
		return errPlatformRoleActionsRequired()
	}
	if existing, err := s.store.GetByName(in.Name); err == nil && existing != nil {
		// Only a hit is meaningful; a not-found error is the happy path and is
		// deliberately not surfaced (the insert itself reports a genuine
		// persistence failure).
		return errPlatformRoleExists()
	}
	// Client-supplied identity is ignored: id / isSystem / createdAt are
	// server-owned, so a caller cannot forge a built-in (immutable) role.
	in.IsSystem = false
	return s.store.Create(in)
}

// Update rewrites name/description/actions of a custom role. Built-ins are
// immutable so a console mistake cannot strip sdp-admin of its actions and
// lock everyone out of the platform.
func (s *PlatformRoleService) Update(id uuid.UUID, in *models.PlatformRole) error {
	cur, err := s.store.GetByID(id)
	if err != nil {
		return errPlatformRoleNotFound()
	}
	if cur.IsSystem {
		return errPlatformRoleImmutable()
	}
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" {
		return errPlatformRoleNameRequired()
	}
	if !validActions(in.Actions) {
		return errPlatformRoleActionsRequired()
	}
	if other, gerr := s.store.GetByName(in.Name); gerr == nil && other != nil && other.ID != id {
		return errPlatformRoleExists()
	}
	// Read-modify-write: Save persists every mapped column, so carry the
	// server-owned fields forward instead of letting them fall back to zero.
	cur.Name = in.Name
	cur.Description = in.Description
	cur.Actions = in.Actions
	return s.store.Update(cur)
}

// Delete removes a custom role, refusing while bindings still reference it
// (409 + reasons) — the same refuse-rather-than-orphan rule the hub's
// deletion contract applies to other resources.
func (s *PlatformRoleService) Delete(id uuid.UUID) error {
	cur, err := s.store.GetByID(id)
	if err != nil {
		return errPlatformRoleNotFound()
	}
	if cur.IsSystem {
		return errPlatformRoleImmutable()
	}
	n, cerr := s.store.CountBindings(id)
	if cerr != nil {
		return cerr
	}
	if n > 0 {
		return errPlatformRoleInUse(n)
	}
	return s.store.Delete(id)
}

// validActions reports whether the JSON column decodes to at least one
// non-blank action string. A role granting nothing is almost always a bug
// (it silently authorizes nobody), so it is rejected at the edge.
func validActions(raw []byte) bool {
	if len(raw) == 0 {
		return false
	}
	var acts []string
	if err := json.Unmarshal(raw, &acts); err != nil {
		return false
	}
	for _, a := range acts {
		if strings.TrimSpace(a) != "" {
			return true
		}
	}
	return false
}
