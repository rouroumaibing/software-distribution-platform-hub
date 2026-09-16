package repository

import (
	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/rouroumaibing/software-distribution-platform-hub/internal/common"
	"github.com/rouroumaibing/software-distribution-platform-hub/internal/run/models"
)

// RolloutRunRepository is read-mostly for progressive-delivery history. Rows
// are normally written by the runner via status sync, but the console needs a
// CRUD surface (P0-1) to view and manage published releases, so Create/Update/
// Delete are exposed through the release service/handler.
type RolloutRunRepository struct{ DB *gorm.DB }

func NewRolloutRunRepository(db *gorm.DB) *RolloutRunRepository {
	return &RolloutRunRepository{DB: db}
}

func (r *RolloutRunRepository) Create(m *models.RolloutRun) error {
	return r.DB.Create(m).Error
}

func (r *RolloutRunRepository) GetByID(id uuid.UUID) (*models.RolloutRun, error) {
	var m models.RolloutRun
	if err := r.DB.First(&m, "id = ?", id).Error; err != nil {
		return nil, err
	}
	return &m, nil
}

// List returns rollout runs ordered by id desc, with an optional filter by
// owning PipelineRun (resolved through task_runs). Pagination matches the rest
// of the API via common.Pagination.
func (r *RolloutRunRepository) List(p common.Pagination, pipelineRunID *uuid.UUID) ([]models.RolloutRun, int64, error) {
	var items []models.RolloutRun
	var total int64
	q := r.DB.Model(&models.RolloutRun{})
	if pipelineRunID != nil {
		q = q.Where("task_run_id IN (SELECT id FROM task_runs WHERE pipeline_run_id = ?)", *pipelineRunID)
	}
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	err := q.Offset(p.Offset()).Limit(p.Limit()).Order("id desc").Find(&items).Error
	return items, total, err
}

func (r *RolloutRunRepository) Update(m *models.RolloutRun) error {
	return r.DB.Save(m).Error
}

func (r *RolloutRunRepository) Delete(id uuid.UUID) error {
	return r.DB.Delete(&models.RolloutRun{}, "id = ?", id).Error
}
