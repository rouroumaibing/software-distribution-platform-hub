package repository

import (
	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/rouroumaibing/software-distribution-platform-hub/internal/common"
	"github.com/rouroumaibing/software-distribution-platform-hub/internal/pipeline/models"
)

type TaskTemplateRepository struct {
	*common.Repository[models.PipelineTaskTemplate]
}

func NewTaskTemplateRepository(db *gorm.DB) *TaskTemplateRepository {
	return &TaskTemplateRepository{common.NewRepository[models.PipelineTaskTemplate](db)}
}

func (r *TaskTemplateRepository) ListByStageID(stageID uuid.UUID) ([]models.PipelineTaskTemplate, error) {
	var tasks []models.PipelineTaskTemplate
	err := r.DB.Where("stage_id = ?", stageID).Order("display_order asc").Find(&tasks).Error
	return tasks, err
}
