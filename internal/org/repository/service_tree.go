package repository

import (
	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/rouroumaibing/software-distribution-platform-hub/internal/common"
	"github.com/rouroumaibing/software-distribution-platform-hub/internal/org/models"
)

type ServiceTreeRepository struct {
	*common.Repository[models.ServiceTree]
}

func NewServiceTreeRepository(db *gorm.DB) *ServiceTreeRepository {
	return &ServiceTreeRepository{common.NewRepository[models.ServiceTree](db)}
}

func (r *ServiceTreeRepository) GetByOrgID(orgID uuid.UUID) (*models.ServiceTree, error) {
	var st models.ServiceTree
	if err := r.DB.First(&st, "org_id = ?", orgID).Error; err != nil {
		return nil, err
	}
	return &st, nil
}
