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

// Create inserts a platform role (C-10: platform admins define roles through
// the API instead of only via seed SQL).
func (r *PlatformRoleRepository) Create(item *models.PlatformRole) error {
	return r.DB.Create(item).Error
}

// Update writes a role back. Callers must read the current row first and
// repopulate every column they do not intend to change: GORM's Save writes
// all mapped columns, so a partially-filled struct would NULL the rest
// (the same trap the hub's service layer already documents elsewhere).
func (r *PlatformRoleRepository) Update(item *models.PlatformRole) error {
	return r.DB.Save(item).Error
}

func (r *PlatformRoleRepository) Delete(id uuid.UUID) error {
	return r.DB.Delete(&models.PlatformRole{}, "id = ?", id).Error
}

// CountBindings reports how many bindings reference the role; a role in use
// must not be deleted (mirrors the repo's own deletion-contract habit of
// refusing rather than orphaning).
func (r *PlatformRoleRepository) CountBindings(id uuid.UUID) (int64, error) {
	var n int64
	err := r.DB.Model(&models.PlatformRoleBinding{}).Where("platform_role_id = ?", id).Count(&n).Error
	return n, err
}
