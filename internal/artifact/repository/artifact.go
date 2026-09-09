package repository

import (
	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/rouroumaibing/software-distribution-platform-hub/internal/artifact/models"
	"github.com/rouroumaibing/software-distribution-platform-hub/internal/common"
)

type ArtifactRepository struct {
	*common.Repository[models.Artifact]
}

func NewArtifactRepository(db *gorm.DB) *ArtifactRepository {
	return &ArtifactRepository{common.NewRepository[models.Artifact](db)}
}

func (r *ArtifactRepository) FindByComponentID(componentID uuid.UUID, p common.Pagination) ([]models.Artifact, int64, error) {
	return r.List(p, func(db *gorm.DB) *gorm.DB {
		return db.Where("component_id = ?", componentID).Order("created_at desc")
	})
}
