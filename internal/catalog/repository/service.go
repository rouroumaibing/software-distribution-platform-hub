package repository

import (
	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/rouroumaibing/software-distribution-platform-hub/internal/catalog/models"
	"github.com/rouroumaibing/software-distribution-platform-hub/internal/common"
)

type ServiceRepository struct {
	*common.Repository[models.Service]
}

func NewServiceRepository(db *gorm.DB) *ServiceRepository {
	return &ServiceRepository{common.NewRepository[models.Service](db)}
}

func (r *ServiceRepository) FindByServiceTreeID(treeID uuid.UUID, p common.Pagination) ([]models.Service, int64, error) {
	return r.List(p, func(db *gorm.DB) *gorm.DB {
		return db.Where("service_tree_id = ?", treeID)
	})
}
