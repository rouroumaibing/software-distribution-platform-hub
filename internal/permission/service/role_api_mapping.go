package service

import (
	"github.com/google/uuid"

	"github.com/rouroumaibing/software-distribution-platform-hub/internal/permission/models"
	"github.com/rouroumaibing/software-distribution-platform-hub/internal/permission/repository"
)

// RoleActionLookup is the slice of the mapping service the binding service
// needs, narrowed so the two stay decoupled (and so tests can stub it).
// *RoleAPIMappingService satisfies it.
type RoleActionLookup interface {
	ActionsForRole(kind string, roleID uuid.UUID) ([]string, error)
}

// RoleAPIMappingService is the authoritative, reviewable registry of which API
// actions a role grants (ACCOUNT-PERMISSION-MODEL §5.1③). The role tables
// still carry an `actions` JSON column; this table mirrors it so the mapping
// is independently auditable and out of the route-registration code.
// SyncFromRoles keeps the two in lock-step.
type RoleAPIMappingService struct {
	repo              *repository.RoleAPIMappingRepository
	platformRoleRepo  *repository.PlatformRoleRepository
	componentRoleRepo *repository.ComponentRoleRepository
}

func NewRoleAPIMappingService(
	repo *repository.RoleAPIMappingRepository,
	platformRoleRepo *repository.PlatformRoleRepository,
	componentRoleRepo *repository.ComponentRoleRepository,
) *RoleAPIMappingService {
	return &RoleAPIMappingService{repo: repo, platformRoleRepo: platformRoleRepo, componentRoleRepo: componentRoleRepo}
}

// ActionsForRole returns the API actions registered for a role. This is merged
// into the role's `actions` JSON by the binding service so the registry is a
// real, consulted source — not a decorative mirror.
func (s *RoleAPIMappingService) ActionsForRole(kind string, roleID uuid.UUID) ([]string, error) {
	rows, err := s.repo.ListByRole(kind, roleID)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.Action)
	}
	return out, nil
}

// SetActions reconciles a role's mapped actions to exactly the given set:
// delete the existing rows for the role, then insert the new ones. Idempotent
// — calling it twice with the same set is a no-op beyond the rewrite.
func (s *RoleAPIMappingService) SetActions(kind string, roleID uuid.UUID, actions []string) error {
	if err := s.repo.DeleteByRole(kind, roleID); err != nil {
		return err
	}
	for _, a := range actions {
		m := &models.RoleAPIMapping{RoleKind: kind, RoleID: roleID, Action: a}
		if err := s.repo.Create(m); err != nil {
			return err
		}
	}
	return nil
}

// SyncFromRoles walks every platform and component role, reads its `actions`
// JSON, and writes the matching rows. Called once at startup (and after any
// role definition change) so the registry never lags the role tables.
func (s *RoleAPIMappingService) SyncFromRoles() error {
	platformRoles, err := s.platformRoleRepo.List()
	if err != nil {
		return err
	}
	for _, r := range platformRoles {
		if serr := s.SetActions("platform", r.ID, unmarshalActions([]byte(r.Actions))); serr != nil {
			return serr
		}
	}
	componentRoles, err := s.componentRoleRepo.List()
	if err != nil {
		return err
	}
	for _, r := range componentRoles {
		if serr := s.SetActions("component", r.ID, unmarshalActions([]byte(r.Actions))); serr != nil {
			return serr
		}
	}
	return nil
}
