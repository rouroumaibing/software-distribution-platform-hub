package repository

import (
	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/rouroumaibing/software-distribution-platform-hub/internal/common"
	"github.com/rouroumaibing/software-distribution-platform-hub/internal/pipeline/models"
)

type PipelineRepository struct {
	*common.Repository[models.Pipeline]
}

func NewPipelineRepository(db *gorm.DB) *PipelineRepository {
	return &PipelineRepository{common.NewRepository[models.Pipeline](db)}
}

func (r *PipelineRepository) FindByComponentID(componentID uuid.UUID, p common.Pagination) ([]models.Pipeline, int64, error) {
	return r.List(p, func(db *gorm.DB) *gorm.DB {
		return db.Where("component_id = ?", componentID)
	})
}
