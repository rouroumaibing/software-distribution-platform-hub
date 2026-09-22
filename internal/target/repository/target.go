package repository

import (
	"gorm.io/gorm"

	"github.com/rouroumaibing/software-distribution-platform-hub/internal/common"
	"github.com/rouroumaibing/software-distribution-platform-hub/internal/target/models"
)

type TargetRepository struct {
	*common.Repository[models.Target]
}

func NewTargetRepository(db *gorm.DB) *TargetRepository {
	return &TargetRepository{common.NewRepository[models.Target](db)}
}

// GetByName resolves a target by its unique Name (the identifier a Runner
// presents as X-Target-Name when it dials the gateway).
func (r *TargetRepository) GetByName(name string) (*models.Target, error) {
	var tg models.Target
	if err := r.DB.First(&tg, "name = ?", name).Error; err != nil {
		return nil, err
	}
	return &tg, nil
}
