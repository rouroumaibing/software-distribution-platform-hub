package repository

import (
	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/rouroumaibing/software-distribution-platform-hub/internal/permission/models"
)

// ComponentRoleRepository backs the §7.3 component-scoped roles. Built-in
// roles (org_id IS NULL) are seeded (09_rbac_multiorg.sql / AutoMigrate);
// org-defined ones are created through the hub API by a platform admin
// (B-11 自定义角色) — before that the table was read-only.
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

// GetByName is the org-agnostic lookup used to resolve a built-in role
// (e.g. "component-admin") during owner bootstrap. Deliberately unscoped:
// built-ins carry org_id IS NULL, and the callers want "the" role by that
// name regardless of org.
func (r *ComponentRoleRepository) GetByName(name string) (*models.ComponentRole, error) {
	var item models.ComponentRole
	err := r.DB.Where("name = ?", name).First(&item).Error
	return &item, err
}

// GetByNameInOrg scopes the name lookup to one org — the uniqueness key for
// custom roles is (org_id, name), so two orgs may both define "release-owner".
//
// orgID == nil is expressed as `org_id IS NULL` rather than `org_id = NULL`:
// the latter is never true in SQL, which would silently disable the
// duplicate check for built-ins instead of failing loudly.
func (r *ComponentRoleRepository) GetByNameInOrg(orgID *uuid.UUID, name string) (*models.ComponentRole, error) {
	q := r.DB.Where("name = ?", name)
	if orgID == nil {
		q = q.Where("org_id IS NULL")
	} else {
		q = q.Where("org_id = ?", *orgID)
	}
	var item models.ComponentRole
	err := q.First(&item).Error
	return &item, err
}

func (r *ComponentRoleRepository) Create(item *models.ComponentRole) error {
	return r.DB.Create(item).Error
}

func (r *ComponentRoleRepository) Update(item *models.ComponentRole) error {
	return r.DB.Save(item).Error
}

func (r *ComponentRoleRepository) Delete(id uuid.UUID) error {
	return r.DB.Delete(&models.ComponentRole{}, "id = ?", id).Error
}

// CountBindings counts ComponentRoleBindings that still point at the role.
// Used to refuse deletion with 409 + reasons rather than orphaning grants
// (the hub's refuse-rather-than-orphan rule; DELETE-CONTRACT §6.4).
func (r *ComponentRoleRepository) CountBindings(roleID uuid.UUID) (int64, error) {
	var n int64
	err := r.DB.Model(&models.ComponentRoleBinding{}).
		Where("component_role_id = ?", roleID).
		Count(&n).Error
	return n, err
}
