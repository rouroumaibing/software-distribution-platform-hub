package repository

import (
	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/rouroumaibing/software-distribution-platform-hub/internal/permission/models"
)

// ComponentRoleRepository is read-mostly: built-in roles are seeded
// (09_rbac_multiorg.sql); org-defined ones are created by an org admin.
type ComponentRoleRepository struct{ DB *gorm.DB }

func NewComponentRoleRepository(db *gorm.DB) *ComponentRoleRepository {
	return &ComponentRoleRepository{DB: db}
}

func (r *ComponentRoleRepository) List() ([]models.ComponentRole, error) {
	var items []models.ComponentRole
	err := r.DB.Find(&items).Error
	return items, err
}

func (r *ComponentRoleRepository) GetByID(id uuid.UUID) (*models.ComponentRole, error) {
	var item models.ComponentRole
	err := r.DB.First(&item, id).Error
	return &item, err
}

func (r *ComponentRoleRepository) GetByName(name string) (*models.ComponentRole, error) {
	var item models.ComponentRole
	err := r.DB.Where("name = ?", name).First(&item).Error
	return &item, err
}
