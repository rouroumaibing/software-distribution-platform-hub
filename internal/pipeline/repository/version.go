package repository

import (
	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/rouroumaibing/software-distribution-platform-hub/internal/pipeline/models"
)

type VersionRepository struct{ DB *gorm.DB }

func NewVersionRepository(db *gorm.DB) *VersionRepository { return &VersionRepository{DB: db} }

func (r *VersionRepository) Create(v *models.PipelineVersion) error { return r.DB.Create(v).Error }

// ListByPipelineID returns a pipeline's snapshots, newest first. limit bounds
// the window (<=0 means "no bound") because a snapshot body carries every
// stage and task — an unbounded list would ship the whole edit history in one
// response.
func (r *VersionRepository) ListByPipelineID(pipelineID uuid.UUID, limit int) ([]models.PipelineVersion, error) {
	var versions []models.PipelineVersion
	q := r.DB.Where("pipeline_id = ?", pipelineID).Order("version desc")
	if limit > 0 {
		q = q.Limit(limit)
	}
	err := q.Find(&versions).Error
	return versions, err
}

// GetByVersion returns one snapshot of a pipeline, addressed by its monotonic
// version number (the console's diff/rollback both address versions this way —
// it is the human-facing identifier, unlike the row uuid which changes if the
// history is ever rebuilt).
func (r *VersionRepository) GetByVersion(pipelineID uuid.UUID, version int) (*models.PipelineVersion, error) {
	var v models.PipelineVersion
	err := r.DB.Where("pipeline_id = ? AND version = ?", pipelineID, version).First(&v).Error
	if err != nil {
		return nil, err
	}
	return &v, nil
}

// Latest returns the highest-numbered snapshot for a pipeline, or
// gorm.ErrRecordNotFound when the pipeline has none yet.
func (r *VersionRepository) Latest(pipelineID uuid.UUID) (*models.PipelineVersion, error) {
	var v models.PipelineVersion
	err := r.DB.Where("pipeline_id = ?", pipelineID).Order("version desc").First(&v).Error
	if err != nil {
		return nil, err
	}
	return &v, nil
}
