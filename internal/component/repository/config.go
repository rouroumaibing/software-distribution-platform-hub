package repository

import (
	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/rouroumaibing/software-distribution-platform-hub/internal/common"
	"github.com/rouroumaibing/software-distribution-platform-hub/internal/component/models"
)

type ComponentConfigRepository struct {
	*common.Repository[models.ComponentConfig]
}

func NewComponentConfigRepository(db *gorm.DB) *ComponentConfigRepository {
	return &ComponentConfigRepository{common.NewRepository[models.ComponentConfig](db)}
}

// FindByComponentID lists global + environment-scoped config for a component.
// Pass environmentID == nil to list only the global defaults.
func (r *ComponentConfigRepository) FindByComponentID(componentID uuid.UUID, environmentID *uuid.UUID, p common.Pagination) ([]models.ComponentConfig, int64, error) {
	return r.List(p, func(db *gorm.DB) *gorm.DB {
		q := db.Where("component_id = ?", componentID)
		if environmentID != nil {
			return q.Where("environment_id = ?", *environmentID)
		}
		return q.Where("environment_id IS NULL")
	})
}

// GetByKey resolves the config row for a (component, key) pair. Pass
// environmentID == nil to match the global (environment-unscoped) row.
func (r *ComponentConfigRepository) GetByKey(componentID uuid.UUID, key string, environmentID *uuid.UUID) (*models.ComponentConfig, error) {
	var cfg models.ComponentConfig
	q := r.DB.Where("component_id = ? AND key = ?", componentID, key)
	if environmentID != nil {
		q = q.Where("environment_id = ?", *environmentID)
	} else {
		q = q.Where("environment_id IS NULL")
	}
	if err := q.First(&cfg).Error; err != nil {
		return nil, err
	}
	return &cfg, nil
}

func (r *ComponentConfigRepository) LogHistory(h *models.ComponentConfigHistory) error {
	return r.DB.Create(h).Error
}
