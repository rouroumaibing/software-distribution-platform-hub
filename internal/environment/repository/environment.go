package repository

import (
	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/rouroumaibing/software-distribution-platform-hub/internal/common"
	"github.com/rouroumaibing/software-distribution-platform-hub/internal/environment/models"
)

type EnvironmentRepository struct {
	*common.Repository[models.Environment]
}

func NewEnvironmentRepository(db *gorm.DB) *EnvironmentRepository {
	return &EnvironmentRepository{common.NewRepository[models.Environment](db)}
}

func (r *EnvironmentRepository) FindByComponentID(componentID uuid.UUID, p common.Pagination) ([]models.Environment, int64, error) {
	return r.List(p, func(db *gorm.DB) *gorm.DB {
		return db.Where("component_id = ?", componentID)
	})
}
