package service

import (
	"encoding/json"

	"github.com/google/uuid"

	"github.com/rouroumaibing/software-distribution-platform-hub/internal/permission/models"
	"github.com/rouroumaibing/software-distribution-platform-hub/internal/permission/repository"
)

type BindingService struct {
	repo     *repository.BindingRepository
	roleRepo *repository.RoleRepository
}

func NewBindingService(repo *repository.BindingRepository, roleRepo *repository.RoleRepository) *BindingService {
	return &BindingService{repo: repo, roleRepo: roleRepo}
}

func (s *BindingService) Create(b *models.ComponentRoleBinding) error { return s.repo.Create(b) }

func (s *BindingService) ListByComponent(componentID uuid.UUID) ([]models.ComponentRoleBinding, error) {
	return s.repo.ListByComponentID(componentID)
}

func (s *BindingService) Delete(id uuid.UUID) error { return s.repo.Delete(id) }

// HasPermission is what middleware calls to authorize a request: does this
// user's role on this component include the requested permission.
func (s *BindingService) HasPermission(componentID, userID uuid.UUID, permission string) (bool, error) {
	binding, err := s.repo.GetByComponentAndUser(componentID, userID)
	if err != nil {
		return false, err
	}
	roles, err := s.roleRepo.List()
	if err != nil {
		return false, err
	}
	for _, role := range roles {
		if role.ID != binding.RoleID {
			continue
		}
		var perms []string
		if err := json.Unmarshal(role.Permissions, &perms); err != nil {
			return false, err
		}
		for _, p := range perms {
			if p == permission {
				return true, nil
			}
		}
	}
	return false, nil
}
