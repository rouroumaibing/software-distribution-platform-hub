package service

import (
	"encoding/json"

	"github.com/google/uuid"

	componentrepo "github.com/rouroumaibing/software-distribution-platform-hub/internal/component/repository"
	"github.com/rouroumaibing/software-distribution-platform-hub/internal/permission/models"
	"github.com/rouroumaibing/software-distribution-platform-hub/internal/permission/repository"
)

type BindingService struct {
	repo              *repository.BindingRepository
	componentRoleRepo *repository.ComponentRoleRepository
	platformRoleRepo  *repository.PlatformRoleRepository
	platformBindRepo  *repository.PlatformRoleBindingRepository
	componentRepo     *componentrepo.ComponentRepository
	// roleActions is the reviewable role→action registry
	// (ACCOUNT-PERMISSION-MODEL §5.1③). Optional (nil-safe): when wired, the
	// effective action set is the union of each role's `actions` JSON and its
	// registered mappings, so the registry is a consulted source rather than a
	// decorative mirror.
	roleActions RoleActionLookup
}

// NewBindingService wires the authorization service. It no longer takes the
// V1 `roles` repository: D3 dropped component_role_bindings.role_id, so the
// legacy per-role-permission branch this service used to resolve has no
// column left to read from.
func NewBindingService(
	repo *repository.BindingRepository,
	componentRoleRepo *repository.ComponentRoleRepository,
	platformRoleRepo *repository.PlatformRoleRepository,
	platformBindRepo *repository.PlatformRoleBindingRepository,
	componentRepo *componentrepo.ComponentRepository,
	roleActions RoleActionLookup,
) *BindingService {
	return &BindingService{
		repo:              repo,
		componentRoleRepo: componentRoleRepo,
		platformRoleRepo:  platformRoleRepo,
		platformBindRepo:  platformBindRepo,
		componentRepo:     componentRepo,
		roleActions:       roleActions,
	}
}

func (s *BindingService) Create(b *models.ComponentRoleBinding) error { return s.repo.Create(b) }

func (s *BindingService) ListByComponent(componentID uuid.UUID) ([]models.ComponentRoleBinding, error) {
	return s.repo.ListByComponentID(componentID)
}

func (s *BindingService) Delete(id uuid.UUID) error { return s.repo.Delete(id) }

// unmarshalActions decodes a role's actions JSON column into a string slice.
func unmarshalActions(raw []byte) []string {
	var acts []string
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &acts)
	}
	return acts
}

// mappedActions returns the actions registered for a role in the mapping table
// (nil-safe: a nil lookup means "not wired", so the call site degrades to the
// JSON column alone rather than erroring).
func (s *BindingService) mappedActions(kind string, roleID uuid.UUID) []string {
	if s.roleActions == nil {
		return nil
	}
	acts, err := s.roleActions.ActionsForRole(kind, roleID)
	if err != nil {
		return nil
	}
	return acts
}

// ResolveComponentActions returns the effective set of component-scoped
// actions the subject (Keycloak `sub` + Keycloak groups) holds on a component,
// merging every matching ComponentRoleBinding (§7 subject model), the
// role→action registry (§5.1③) and the component-owner override (§7.4).
// Duplicate actions are collapsed.
//
// The V1 legacy resolution path is gone: D3 dropped component_role_bindings
// .user_id/.role_id together with the local `users` table, so every binding
// the query can return is already a §7 subject binding.
func (s *BindingService) ResolveComponentActions(componentID uuid.UUID, subject string, groups []string) ([]string, error) {
	seen := map[string]struct{}{}
	var actions []string
	add := func(a []string) {
		for _, x := range a {
			if _, ok := seen[x]; !ok {
				seen[x] = struct{}{}
				actions = append(actions, x)
			}
		}
	}

	bindings, err := s.repo.ListMatching(componentID, subject, groups)
	if err != nil {
		return nil, err
	}
	for _, b := range bindings {
		if b.ComponentRoleID != nil {
			cr, cerr := s.componentRoleRepo.GetByID(*b.ComponentRoleID)
			if cerr != nil {
				continue
			}
			add(unmarshalActions(cr.Actions))
			add(s.mappedActions("component", *b.ComponentRoleID))
		}
	}

	// Owner override (§7.4): the component owner (subject or group) gets the
	// full component-admin action set even before an explicit binding is
	// created (P3b auto-binds the owner too, this is the safety net).
	if ownerActs, oerr := s.ownerActions(componentID, subject, groups); oerr == nil {
		add(ownerActs)
	}
	return actions, nil
}

// ownerActions returns the component-admin action set when the subject is the
// component's owner, else nil. OwnerSub holds a Keycloak `sub` (§5.3), so the
// comparison is a plain string equality against the request subject — the
// local-uuid comparison D3 removed.
func (s *BindingService) ownerActions(componentID uuid.UUID, subject string, groups []string) ([]string, error) {
	if s.componentRepo == nil {
		return nil, nil
	}
	c, err := s.componentRepo.GetByID(componentID)
	if err != nil {
		return nil, err
	}
	isOwner := (c.OwnerSub != nil && *c.OwnerSub == subject) ||
		(c.OwnerGroup != nil && contains(groups, *c.OwnerGroup))
	if !isOwner {
		return nil, nil
	}
	admin, err := s.componentRoleRepo.GetByName("component-admin")
	if err != nil {
		return nil, err
	}
	return unmarshalActions(admin.Actions), nil
}

func contains(haystack []string, needle string) bool {
	for _, h := range haystack {
		if h == needle {
			return true
		}
	}
	return false
}

// HasPermission is what middleware calls to authorize a request: does this
// subject's effective role set on this component include the requested §7
// action (DATA-MODEL §7.3)?
func (s *BindingService) HasPermission(componentID uuid.UUID, subject string, groups []string, permission string) (bool, error) {
	actions, err := s.ResolveComponentActions(componentID, subject, groups)
	if err != nil {
		return false, err
	}
	for _, a := range actions {
		if a == permission {
			return true, nil
		}
	}
	return false, nil
}

// HasPlatformPermission checks a platform-level §7 action (page/console
// visibility, org/user management) for a subject, scoped to an org. The
// subject is the Keycloak `sub`; orgID == nil means "global bindings only".
func (s *BindingService) HasPlatformPermission(orgID *uuid.UUID, subject string, groups []string, permission string) (bool, error) {
	bindings, err := s.platformBindRepo.ListMatching(orgID, subject, groups)
	if err != nil {
		return false, err
	}
	for _, b := range bindings {
		pr, perr := s.platformRoleRepo.GetByID(b.PlatformRoleID)
		if perr != nil {
			continue
		}
		for _, a := range unmarshalActions(pr.Actions) {
			if a == permission {
				return true, nil
			}
		}
		for _, a := range s.mappedActions("platform", b.PlatformRoleID) {
			if a == permission {
				return true, nil
			}
		}
	}
	return false, nil
}
