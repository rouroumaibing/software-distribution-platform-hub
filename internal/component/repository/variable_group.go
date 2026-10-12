package repository

import (
	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/rouroumaibing/software-distribution-platform-hub/internal/component/models"
)

// VariableGroupRepository is the store for org-scoped shared variable groups
// and their per-component bindings (ui-12 §5.3, N-26).
type VariableGroupRepository struct {
	db *gorm.DB
}

func NewVariableGroupRepository(db *gorm.DB) *VariableGroupRepository {
	return &VariableGroupRepository{db: db}
}

func (r *VariableGroupRepository) ListByOrg(orgID uuid.UUID) ([]models.VariableGroup, error) {
	var rows []models.VariableGroup
	err := r.db.Where("org_id = ?", orgID).Order("name").Find(&rows).Error
	return rows, err
}

func (r *VariableGroupRepository) Get(id uuid.UUID) (*models.VariableGroup, error) {
	var row models.VariableGroup
	if err := r.db.First(&row, "id = ?", id).Error; err != nil {
		return nil, err
	}
	return &row, nil
}

func (r *VariableGroupRepository) Create(row *models.VariableGroup) error {
	return r.db.Create(row).Error
}

func (r *VariableGroupRepository) Update(row *models.VariableGroup) error {
	return r.db.Save(row).Error
}

func (r *VariableGroupRepository) Delete(id uuid.UUID) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		// Bindings referencing the group are removed with it: a dangling
		// binding would 500 the trigger-time resolver.
		if err := tx.Where("group_id = ?", id).Delete(&models.ComponentVariableGroupBinding{}).Error; err != nil {
			return err
		}
		return tx.Delete(&models.VariableGroup{}, "id = ?", id).Error
	})
}

// ---------- bindings ----------

func (r *VariableGroupRepository) ListBindings(componentID uuid.UUID) ([]models.ComponentVariableGroupBinding, error) {
	var rows []models.ComponentVariableGroupBinding
	err := r.db.Where("component_id = ?", componentID).Order("priority").Find(&rows).Error
	return rows, err
}

// ListBoundGroups returns the groups bound to a component, ordered by
// ascending priority — the resolver applies them in this order.
func (r *VariableGroupRepository) ListBoundGroups(componentID uuid.UUID) ([]models.VariableGroup, error) {
	var rows []models.VariableGroup
	err := r.db.
		Joins("JOIN component_variable_group_bindings b ON b.group_id = variable_groups.id").
		Where("b.component_id = ?", componentID).
		Order("b.priority ASC").
		Find(&rows).Error
	return rows, err
}

func (r *VariableGroupRepository) CreateBinding(b *models.ComponentVariableGroupBinding) error {
	return r.db.Create(b).Error
}

func (r *VariableGroupRepository) DeleteBinding(id uuid.UUID) error {
	return r.db.Delete(&models.ComponentVariableGroupBinding{}, "id = ?", id).Error
}
