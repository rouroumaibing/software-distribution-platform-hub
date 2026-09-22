package repository

import (
	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/rouroumaibing/software-distribution-platform-hub/internal/common"
	"github.com/rouroumaibing/software-distribution-platform-hub/internal/permission/models"
)

type RoleAPIMappingRepository struct {
	*common.Repository[models.RoleAPIMapping]
}

func NewRoleAPIMappingRepository(db *gorm.DB) *RoleAPIMappingRepository {
	return &RoleAPIMappingRepository{common.NewRepository[models.RoleAPIMapping](db)}
}

// ListByRole returns every API action mapped to a role (platform | component).
func (r *RoleAPIMappingRepository) ListByRole(kind string, roleID uuid.UUID) ([]models.RoleAPIMapping, error) {
	var rows []models.RoleAPIMapping
	if err := r.DB.Where("role_kind = ? AND role_id = ?", kind, roleID).Find(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

// DeleteByRole removes every mapping for a role, used by the idempotent
// SyncFromRoles reconcile so the table never accumulates stale actions.
func (r *RoleAPIMappingRepository) DeleteByRole(kind string, roleID uuid.UUID) error {
	return r.DB.Where("role_kind = ? AND role_id = ?", kind, roleID).Delete(&models.RoleAPIMapping{}).Error
}
