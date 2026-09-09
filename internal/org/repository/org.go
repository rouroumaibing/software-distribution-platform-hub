package repository

import (
	"gorm.io/gorm"

	"github.com/rouroumaibing/software-distribution-platform-hub/internal/common"
	"github.com/rouroumaibing/software-distribution-platform-hub/internal/org/models"
)

type OrgRepository struct{ *common.Repository[models.Org] }

func NewOrgRepository(db *gorm.DB) *OrgRepository {
	return &OrgRepository{common.NewRepository[models.Org](db)}
}
