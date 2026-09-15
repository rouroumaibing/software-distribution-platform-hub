package repository

import (
	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/rouroumaibing/software-distribution-platform-hub/internal/permission/models"
)

type BindingRepository struct{ DB *gorm.DB }

func NewBindingRepository(db *gorm.DB) *BindingRepository { return &BindingRepository{DB: db} }

func (r *BindingRepository) Create(b *models.ComponentRoleBinding) error { return r.DB.Create(b).Error }

func (r *BindingRepository) ListByComponentID(componentID uuid.UUID) ([]models.ComponentRoleBinding, error) {
	var bindings []models.ComponentRoleBinding
	err := r.DB.Where("component_id = ?", componentID).Find(&bindings).Error
	return bindings, err
}

func (r *BindingRepository) Delete(id uuid.UUID) error {
	return r.DB.Delete(&models.ComponentRoleBinding{}, "id = ?", id).Error
}

// GetByComponentAndUser is what middleware calls on every authenticated
// request to check "does this user have any role on this component".
func (r *BindingRepository) GetByComponentAndUser(componentID, userID uuid.UUID) (*models.ComponentRoleBinding, error) {
	var b models.ComponentRoleBinding
	err := r.DB.Where("component_id = ? AND user_id = ?", componentID, userID).First(&b).Error
	if err != nil {
		return nil, err
	}
	return &b, nil
}

// ListMatching returns every ComponentRoleBinding on a component that
// applies to the subject, merging §7 subject bindings (user/group) with V1
// legacy per-user bindings (user_id set, subject_type empty). Group
// bindings are only included when the subject actually belongs to the group,
// so an empty groups slice simply skips the group branch.
func (r *BindingRepository) ListMatching(componentID uuid.UUID, userID uuid.UUID, groups []string) ([]models.ComponentRoleBinding, error) {
	var bindings []models.ComponentRoleBinding
	userStr := userID.String()
	conds := r.DB.Where("subject_type = ? AND subject_id = ?", "user", userStr).
		Or("user_id = ?", userID) // V1 legacy rows
	if len(groups) > 0 {
		conds = conds.Or("subject_type = ? AND subject_id IN ?", "group", groups)
	}
	err := r.DB.Where("component_id = ?", componentID).Where(conds).Find(&bindings).Error
	return bindings, err
}
