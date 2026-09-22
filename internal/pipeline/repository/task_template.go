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

// DeleteByStageID soft-deletes every template under a stage. Used by
// StageService.Delete as the service-level cascade: the DDL's
// `stage_id ... on delete cascade` only fires on a hard delete, and both
// pipeline_stages and pipeline_task_templates are soft-deleted now, so the
// cascade must be done explicitly or templates stay alive under a deleted
// stage. GORM turns this Delete into `UPDATE ... SET deleted_at = now()`
// because PipelineTaskTemplate embeds common.Base (has gorm.DeletedAt).
func (r *TaskTemplateRepository) DeleteByStageID(stageID uuid.UUID) error {
	return r.DB.Where("stage_id = ?", stageID).Delete(&models.PipelineTaskTemplate{}).Error
}
