package repository

import (
	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/rouroumaibing/software-distribution-platform-hub/internal/common"
	"github.com/rouroumaibing/software-distribution-platform-hub/internal/pipeline/models"
)

type StageRepository struct {
	*common.Repository[models.PipelineStage]
}

func NewStageRepository(db *gorm.DB) *StageRepository {
	return &StageRepository{common.NewRepository[models.PipelineStage](db)}
}

func (r *StageRepository) ListByPipelineID(pipelineID uuid.UUID) ([]models.PipelineStage, error) {
	var stages []models.PipelineStage
	err := r.DB.Where("pipeline_id = ?", pipelineID).Order("sequence asc").Find(&stages).Error
	return stages, err
}
