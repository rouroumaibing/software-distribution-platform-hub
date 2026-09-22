package repository

import (
	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/rouroumaibing/software-distribution-platform-hub/internal/common"
	"github.com/rouroumaibing/software-distribution-platform-hub/internal/permission/models"
)

type ResourceOwnershipRepository struct {
	*common.Repository[models.ResourceOwnership]
}

func NewResourceOwnershipRepository(db *gorm.DB) *ResourceOwnershipRepository {
	return &ResourceOwnershipRepository{common.NewRepository[models.ResourceOwnership](db)}
}

// Get returns the ownership row for a (type, id) pair, or gorm.ErrRecordNotFound.
func (r *ResourceOwnershipRepository) Get(resourceType string, resourceID uuid.UUID) (*models.ResourceOwnership, error) {
	var o models.ResourceOwnership
	if err := r.DB.Where("resource_type = ? AND resource_id = ?", resourceType, resourceID).First(&o).Error; err != nil {
		return nil, err
	}
	return &o, nil
}

// Upsert writes the ownership row, replacing any existing one for the same
// (type, id). Ownership is single-valued per resource (one owner + a share
// set), so an INSERT ... ON CONFLICT keep-it-simple approach is the model's
// invariant.
func (r *ResourceOwnershipRepository) Upsert(o *models.ResourceOwnership) error {
	var existing models.ResourceOwnership
	err := r.DB.Where("resource_type = ? AND resource_id = ?", o.ResourceType, o.ResourceID).First(&existing).Error
	if err == gorm.ErrRecordNotFound {
		return r.DB.Create(o).Error
	}
	if err != nil {
		return err
	}
	o.ID = existing.ID
	return r.DB.Save(o).Error
}

// ListByOrg returns every resource owned by, or shared with, the given org.
// Used by the ownership decision and by org-level admin views.
func (r *ResourceOwnershipRepository) ListByOrg(orgID uuid.UUID) ([]models.ResourceOwnership, error) {
	var rows []models.ResourceOwnership
	// owner_org match, plus the JSON containment test for the share set.
	err := r.DB.
		Where("owner_org = ?", orgID).
		Or("allowed_orgs::jsonb ? ?", orgID.String()).
		Find(&rows).Error
	return rows, err
}
