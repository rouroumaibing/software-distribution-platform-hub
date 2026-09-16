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

// FindAll returns a cross-component (global) pipeline list, optionally filtered
// by owning component, kind (build/release/custom) or a name substring. It is
// the backend half of the resource index the console currently aggregates in
// the browser (see console/src/composables/useResourceMap.ts) and the basis for
// the future ⌘K global search (P1-2).
func (r *PipelineRepository) FindAll(componentID uuid.UUID, kind, name string, p common.Pagination) ([]models.Pipeline, int64, error) {
	return r.List(p, func(db *gorm.DB) *gorm.DB {
		if componentID != uuid.Nil {
			db = db.Where("component_id = ?", componentID)
		}
		if kind != "" {
			db = db.Where("kind = ?", kind)
		}
		if name != "" {
			db = db.Where("name ILIKE ?", "%"+name+"%")
		}
		return db.Order("created_at desc")
	})
}
