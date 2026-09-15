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
	roleRepo          *repository.RoleRepository
	componentRoleRepo *repository.ComponentRoleRepository
	platformRoleRepo  *repository.PlatformRoleRepository
	platformBindRepo  *repository.PlatformRoleBindingRepository
	componentRepo     *componentrepo.ComponentRepository
}

func NewBindingService(
	repo *repository.BindingRepository,
	roleRepo *repository.RoleRepository,
	componentRoleRepo *repository.ComponentRoleRepository,
	platformRoleRepo *repository.PlatformRoleRepository,
	platformBindRepo *repository.PlatformRoleBindingRepository,
	componentRepo *componentrepo.ComponentRepository,
) *BindingService {
	return &BindingService{
		repo:              repo,
		roleRepo:          roleRepo,
		componentRoleRepo: componentRoleRepo,
		platformRoleRepo:  platformRoleRepo,
		platformBindRepo:  platformBindRepo,
		componentRepo:     componentRepo,
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

// ResolveComponentActions returns the effective set of component-scoped
// actions the subject (user + Keycloak groups) holds on a component, merging
// every matching ComponentRoleBinding (§7 subject model + V1 legacy) and the
// component-owner override (§7.4). Duplicate actions are collapsed.
func (s *BindingService) ResolveComponentActions(componentID uuid.UUID, userID uuid.UUID, groups []string) ([]string, error) {
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

	bindings, err := s.repo.ListMatching(componentID, userID, groups)
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
		} else if b.RoleID != nil {
			// V1 legacy: resolve the roles table's permissions jsonb.
			role, rerr := s.roleRepo.GetByID(*b.RoleID)
			if rerr != nil {
				continue
			}
			add(unmarshalActions(role.Permissions))
		}
	}

	// Owner override (§7.4): the component owner (user or group) gets the
	// full component-admin action set even before an explicit binding is
	// created (P3b auto-binds the owner too, this is the safety net).
	if ownerActs, oerr := s.ownerActions(componentID, userID, groups); oerr == nil {
		add(ownerActs)
	}
	return actions, nil
}

// ownerActions returns the component-admin action set when the subject is the
// component's owner, else nil.
func (s *BindingService) ownerActions(componentID uuid.UUID, userID uuid.UUID, groups []string) ([]string, error) {
	if s.componentRepo == nil {
		return nil, nil
	}
	c, err := s.componentRepo.GetByID(componentID)
	if err != nil {
		return nil, err
	}
	isOwner := (c.OwnerUser != nil && *c.OwnerUser == userID) ||
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
func (s *BindingService) HasPermission(componentID, userID uuid.UUID, groups []string, permission string) (bool, error) {
	actions, err := s.ResolveComponentActions(componentID, userID, groups)
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
// visibility, org/user management) for a subject, scoped to an org.
func (s *BindingService) HasPlatformPermission(orgID *uuid.UUID, userID uuid.UUID, groups []string, permission string) (bool, error) {
	bindings, err := s.platformBindRepo.ListMatching(orgID, userID, groups)
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
	}
	return false, nil
}
