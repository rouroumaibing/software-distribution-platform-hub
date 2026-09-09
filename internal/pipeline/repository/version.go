package repository

import (
	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/rouroumaibing/software-distribution-platform-hub/internal/pipeline/models"
)

type VersionRepository struct{ DB *gorm.DB }

func NewVersionRepository(db *gorm.DB) *VersionRepository { return &VersionRepository{DB: db} }

func (r *VersionRepository) Create(v *models.PipelineVersion) error { return r.DB.Create(v).Error }

func (r *VersionRepository) ListByPipelineID(pipelineID uuid.UUID) ([]models.PipelineVersion, error) {
	var versions []models.PipelineVersion
	err := r.DB.Where("pipeline_id = ?", pipelineID).Order("version desc").Find(&versions).Error
	return versions, err
}
