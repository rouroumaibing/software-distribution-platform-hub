package repository

import (
	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/rouroumaibing/software-distribution-platform-hub/internal/common"
	"github.com/rouroumaibing/software-distribution-platform-hub/internal/component/models"
)

// ComponentRepository embeds the generic CRUD repository and adds
// component-specific queries.
type ComponentRepository struct {
	*common.Repository[models.Component]
}

func NewComponentRepository(db *gorm.DB) *ComponentRepository {
	return &ComponentRepository{common.NewRepository[models.Component](db)}
}

// FindByServiceID lists every component under a given service.
func (r *ComponentRepository) FindByServiceID(serviceID uuid.UUID, p common.Pagination) ([]models.Component, int64, error) {
	return r.List(p, func(db *gorm.DB) *gorm.DB {
		return db.Where("service_id = ?", serviceID)
	})
}

// GetOrgID resolves the org a component belongs to via
// component → service → service_tree. Used to denormalize org_id onto
// RBAC rows (DATA-MODEL §7).
func (r *ComponentRepository) GetOrgID(componentID uuid.UUID) (uuid.UUID, error) {
	var orgID uuid.UUID
	err := r.DB.Raw(`
		SELECT st.org_id FROM components c
		JOIN services s ON s.id = c.service_id
		JOIN service_trees st ON st.id = s.service_tree_id
		WHERE c.id = ?`, componentID).Scan(&orgID).Error
	return orgID, err
}
