package repository

import (
	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/rouroumaibing/software-distribution-platform-hub/internal/permission/models"
)

// RoleRepository is read-only from the API's perspective — roles are
// seeded via migration (Viewer/Editor/Admin) or created by an org admin
// through a separate, more guarded flow, not exposed here.
type RoleRepository struct{ DB *gorm.DB }

func NewRoleRepository(db *gorm.DB) *RoleRepository { return &RoleRepository{DB: db} }

func (r *RoleRepository) List() ([]models.Role, error) {
	var roles []models.Role
	err := r.DB.Find(&roles).Error
	return roles, err
}

func (r *RoleRepository) GetByID(id uuid.UUID) (*models.Role, error) {
	var role models.Role
	err := r.DB.First(&role, id).Error
	return &role, err
}
