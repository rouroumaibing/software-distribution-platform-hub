package service

import (
	"fmt"

	"github.com/google/uuid"

	"github.com/rouroumaibing/software-distribution-platform-hub/internal/component/models"
	"github.com/rouroumaibing/software-distribution-platform-hub/internal/component/repository"
)

// VariableGroupService manages org-scoped shared variable groups and their
// component bindings (ui-12 §5.3, N-26), and exposes the trigger-time
// resolver that flattens a component's bound groups into params.
type VariableGroupService struct {
	repo *repository.VariableGroupRepository
}

func NewVariableGroupService(repo *repository.VariableGroupRepository) *VariableGroupService {
	return &VariableGroupService{repo: repo}
}

func (s *VariableGroupService) ListByOrg(orgID uuid.UUID) ([]models.VariableGroup, error) {
	return s.repo.ListByOrg(orgID)
}

func (s *VariableGroupService) Get(id uuid.UUID) (*models.VariableGroup, error) {
	return s.repo.Get(id)
}

func (s *VariableGroupService) Create(row *models.VariableGroup, by string) error {
	if row.Name == "" {
		return fmt.Errorf("name 必填")
	}
	row.CreatedBy = by
	return s.repo.Create(row)
}

func (s *VariableGroupService) Update(row *models.VariableGroup) error {
	if row.Name == "" {
		return fmt.Errorf("name 必填")
	}
	return s.repo.Update(row)
}

// Delete removes the group and its bindings atomically (repository-level).
func (s *VariableGroupService) Delete(id uuid.UUID) error {
	return s.repo.Delete(id)
}

func (s *VariableGroupService) ListBindings(componentID uuid.UUID) ([]models.ComponentVariableGroupBinding, error) {
	return s.repo.ListBindings(componentID)
}

func (s *VariableGroupService) Bind(componentID, groupID uuid.UUID, priority int, by string) error {
	return s.repo.CreateBinding(&models.ComponentVariableGroupBinding{
		ComponentID: componentID,
		GroupID:     groupID,
		Priority:    priority,
		CreatedBy:   by,
	})
}

func (s *VariableGroupService) Unbind(bindingID uuid.UUID) error {
	return s.repo.DeleteBinding(bindingID)
}

// ComponentGroupParams flattens the component's bound groups (ascending
// priority, later wins on duplicate keys) into trigger-time params. Pure
// pass-through over GroupVariableParams so the resolver is unit-testable at
// the model layer.
func (s *VariableGroupService) ComponentGroupParams(componentID uuid.UUID) ([]models.VariableEntry, error) {
	groups, err := s.repo.ListBoundGroups(componentID)
	if err != nil {
		return nil, err
	}
	flat := models.GroupVariableParams(groups)
	out := make([]models.VariableEntry, 0, len(flat))
	for _, f := range flat {
		out = append(out, models.VariableEntry{Key: f.Name, Value: f.Value})
	}
	return out, nil
}
