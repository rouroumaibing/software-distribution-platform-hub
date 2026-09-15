package repository

import (
	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/rouroumaibing/software-distribution-platform-hub/internal/run/models"
)

// PipelineApprovalRepository persists hub-side approval records
// (DATA-MODEL §7.4). The hub owns the audit trail + self-review gate;
// the runner still owns the DAG pause (it records status on the TaskRun).
type PipelineApprovalRepository struct{ DB *gorm.DB }

func NewPipelineApprovalRepository(db *gorm.DB) *PipelineApprovalRepository {
	return &PipelineApprovalRepository{DB: db}
}

func (r *PipelineApprovalRepository) Create(a *models.PipelineApproval) error {
	return r.DB.Create(a).Error
}

// GetByRunAndTask returns the single approval row for a DAG node, used by
// the approval gating flow to enforce the status machine + self-review.
func (r *PipelineApprovalRepository) GetByRunAndTask(runID, taskRunID uuid.UUID) (*models.PipelineApproval, error) {
	var a models.PipelineApproval
	err := r.DB.Where("run_id = ? AND task_run_id = ?", runID, taskRunID).First(&a).Error
	return &a, err
}

func (r *PipelineApprovalRepository) Update(a *models.PipelineApproval) error {
	return r.DB.Save(a).Error
}

func (r *PipelineApprovalRepository) ListByRun(runID uuid.UUID) ([]models.PipelineApproval, error) {
	var items []models.PipelineApproval
	err := r.DB.Where("run_id = ?", runID).Order("created_at asc").Find(&items).Error
	return items, err
}
