package repository

import (
	"gorm.io/gorm"

	"github.com/rouroumaibing/software-distribution-platform-hub/internal/common"
	"github.com/rouroumaibing/software-distribution-platform-hub/internal/permission/models"
)

type PermissionRequestRepository struct {
	*common.Repository[models.PermissionRequest]
}

func NewPermissionRequestRepository(db *gorm.DB) *PermissionRequestRepository {
	return &PermissionRequestRepository{common.NewRepository[models.PermissionRequest](db)}
}

// ListByStatus returns all requests in a given lifecycle state (Pending, etc.).
func (r *PermissionRequestRepository) ListByStatus(status string) ([]models.PermissionRequest, error) {
	return r.Repository.ListWhere("status = ?", status)
}

// ListAll returns every request, newest first. Backs the management list when
// no status filter is supplied.
func (r *PermissionRequestRepository) ListAll() ([]models.PermissionRequest, error) {
	var items []models.PermissionRequest
	if err := r.DB.Order("created_at DESC").Find(&items).Error; err != nil {
		return nil, err
	}
	return items, nil
}

// ListByRequester returns every request filed by a subject (sub).
func (r *PermissionRequestRepository) ListByRequester(sub string) ([]models.PermissionRequest, error) {
	return r.Repository.ListWhere("requester = ?", sub)
}
