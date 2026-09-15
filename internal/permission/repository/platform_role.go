package repository

import (
	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/rouroumaibing/software-distribution-platform-hub/internal/permission/models"
)

// PlatformRoleRepository is read-mostly: built-in roles are seeded
// (09_rbac_multiorg.sql); org-defined ones are created by a platform admin.
type PlatformRoleRepository struct{ DB *gorm.DB }

func NewPlatformRoleRepository(db *gorm.DB) *PlatformRoleRepository {
	return &PlatformRoleRepository{DB: db}
}

func (r *PlatformRoleRepository) List() ([]models.PlatformRole, error) {
	var items []models.PlatformRole
	err := r.DB.Find(&items).Error
	return items, err
}

func (r *PlatformRoleRepository) GetByID(id uuid.UUID) (*models.PlatformRole, error) {
	var item models.PlatformRole
	err := r.DB.First(&item, id).Error
	return &item, err
}

func (r *PlatformRoleRepository) GetByName(name string) (*models.PlatformRole, error) {
	var item models.PlatformRole
	err := r.DB.Where("name = ?", name).First(&item).Error
	return &item, err
}
