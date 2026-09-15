package repository

import (
	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/rouroumaibing/software-distribution-platform-hub/internal/permission/models"
)

// PlatformRoleBindingRepository links subjects (user|group) to platform
// roles. ListMatching resolves every binding that applies to a subject,
// scoped to an org (nil org_id = global role bindings), merging user and
// group subjects like ComponentRoleBinding.ListMatching.
type PlatformRoleBindingRepository struct{ DB *gorm.DB }

func NewPlatformRoleBindingRepository(db *gorm.DB) *PlatformRoleBindingRepository {
	return &PlatformRoleBindingRepository{DB: db}
}

// ListMatching returns the platform-role bindings applying to (orgID, user,
// groups). Global bindings (org_id IS NULL) are always included; org-scoped
// bindings only when orgID is non-nil and matches.
func (r *PlatformRoleBindingRepository) ListMatching(orgID *uuid.UUID, userID uuid.UUID, groups []string) ([]models.PlatformRoleBinding, error) {
	var bindings []models.PlatformRoleBinding
	userStr := userID.String()
	conds := r.DB.Where("subject_type = ? AND subject_id = ?", "user", userStr)
	if len(groups) > 0 {
		conds = conds.Or("subject_type = ? AND subject_id IN ?", "group", groups)
	}
	q := r.DB.Where(conds)
	if orgID != nil {
		q = q.Where("org_id IS NULL OR org_id = ?", *orgID)
	} else {
		q = q.Where("org_id IS NULL")
	}
	err := q.Find(&bindings).Error
	return bindings, err
}
