package repository

import (
	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/rouroumaibing/software-distribution-platform-hub/internal/common"
	"github.com/rouroumaibing/software-distribution-platform-hub/internal/environmentgroup/models"
)

type EnvironmentGroupRepository struct {
	*common.Repository[models.EnvironmentGroup]
}

func NewEnvironmentGroupRepository(db *gorm.DB) *EnvironmentGroupRepository {
	return &EnvironmentGroupRepository{common.NewRepository[models.EnvironmentGroup](db)}
}

func (r *EnvironmentGroupRepository) FindByComponent(componentID uuid.UUID, p common.Pagination) ([]models.EnvironmentGroup, int64, error) {
	return r.List(p, func(db *gorm.DB) *gorm.DB {
		return db.Where("component_id = ?", componentID).Order("order_index asc, created_at asc")
	})
}

// CountEnvironments returns how many environments reference the group.
func (r *EnvironmentGroupRepository) CountEnvironments(groupID uuid.UUID) (int64, error) {
	var n int64
	if err := r.DB.Table("environments").Where("group_id = ?", groupID).Count(&n).Error; err != nil {
		return 0, err
	}
	return n, nil
}
