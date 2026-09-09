package repository

import (
	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/rouroumaibing/software-distribution-platform-hub/internal/common"
	"github.com/rouroumaibing/software-distribution-platform-hub/internal/run/models"
)

type PipelineRunRepository struct {
	*common.Repository[models.PipelineRun]
}

func NewPipelineRunRepository(db *gorm.DB) *PipelineRunRepository {
	return &PipelineRunRepository{common.NewRepository[models.PipelineRun](db)}
}

func (r *PipelineRunRepository) FindByPipelineID(pipelineID uuid.UUID, p common.Pagination) ([]models.PipelineRun, int64, error) {
	return r.List(p, func(db *gorm.DB) *gorm.DB {
		return db.Where("pipeline_id = ?", pipelineID).Order("created_at desc")
	})
}

// FindAll lists runs across all pipelines (Run Center). phase is an optional
// exact-match filter on the run phase (e.g. "Failed", "Running").
func (r *PipelineRunRepository) FindAll(p common.Pagination, phase string) ([]models.PipelineRun, int64, error) {
	return r.List(p, func(db *gorm.DB) *gorm.DB {
		if phase != "" {
			db = db.Where("phase = ?", phase)
		}
		return db.Order("created_at desc")
	})
}

// GetByCRNameCluster finds a run by its in-cluster CR name + owning cluster,
// used when a Runner reports status back over the gateway.
func (r *PipelineRunRepository) GetByCRNameCluster(crName string, clusterID uuid.UUID) (*models.PipelineRun, error) {
	var run models.PipelineRun
	if err := r.DB.First(&run, "cr_name = ? AND cluster_id = ?", crName, clusterID).Error; err != nil {
		return nil, err
	}
	return &run, nil
}

// CountByPipeline returns how many PipelineRun rows reference a pipeline, used
// by the pipeline service to block deletion while run history still exists.
func (r *PipelineRunRepository) CountByPipeline(pipelineID uuid.UUID) (int64, error) {
	var n int64
	if err := r.DB.Model(&models.PipelineRun{}).Where("pipeline_id = ?", pipelineID).Count(&n).Error; err != nil {
		return 0, err
	}
	return n, nil
}
