package repository

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/rouroumaibing/software-distribution-platform-hub/internal/permission/models"
)

type BindingRepository struct{ DB *gorm.DB }

func NewBindingRepository(db *gorm.DB) *BindingRepository { return &BindingRepository{DB: db} }

func (r *BindingRepository) Create(b *models.ComponentRoleBinding) error { return r.DB.Create(b).Error }

func (r *BindingRepository) ListByComponentID(componentID uuid.UUID) ([]models.ComponentRoleBinding, error) {
	var bindings []models.ComponentRoleBinding
	err := r.DB.Where("component_id = ?", componentID).Find(&bindings).Error
	return bindings, err
}

func (r *BindingRepository) Delete(id uuid.UUID) error {
	return r.DB.Delete(&models.ComponentRoleBinding{}, "id = ?", id).Error
}

// expiredScope narrows a query to rows whose expires_at is a past timestamp.
// Shared by both binding tables' DeleteExpired and by the DryRun regression
// test, so the predicate under test is exactly the one production emits.
func expiredScope(q *gorm.DB, now time.Time) *gorm.DB {
	return q.Where("expires_at IS NOT NULL AND expires_at < ?", now)
}

// DeleteExpired physically removes grants whose expires_at is in the past.
// Authorization already ignores them (ListMatching filters on expiry), so this
// is the periodic cleanup that keeps the table from growing without bound
// (ACCOUNT-PERMISSION-MODEL §7.4).
func (r *BindingRepository) DeleteExpired(now time.Time) (int64, error) {
	res := expiredScope(r.DB, now).Delete(&models.ComponentRoleBinding{})
	return res.RowsAffected, res.Error
}

// GetByComponentAndUser is what the legacy middleware path calls to check
// "does this user have any role on this component". Kept for the V1 column.
func (r *BindingRepository) GetByComponentAndUser(componentID, userID uuid.UUID) (*models.ComponentRoleBinding, error) {
	var b models.ComponentRoleBinding
	err := r.DB.Where("component_id = ? AND user_id = ?", componentID, userID).First(&b).Error
	if err != nil {
		return nil, err
	}
	return &b, nil
}

// ListMatching returns every ComponentRoleBinding on a component that
// applies to the subject, merging §7 subject bindings (user/group) with V1
// legacy per-user bindings (user_id set, subject_type empty). Group
// bindings are only included when the subject actually belongs to the group,
// so an empty groups slice simply skips the group branch.
//
// The user branch matches on subject_id = subject (the Keycloak `sub`), per
// ACCOUNT-PERMISSION-MODEL §5.3; the legacy branch (user_id = local uuid) is
// retained for rows written before the D3 subject migration.
//
// Expired grants (expires_at in the past) are excluded for the same reason as
// in PlatformRoleBindingRepository.ListMatching: this is the single path the
// authorization middleware resolves bindings through, so filtering anywhere
// else would leave an expired grant authorizing requests. now is passed in
// so the emitted SQL stays deterministic for the DryRun regression test.
func (r *BindingRepository) ListMatching(componentID uuid.UUID, userID uuid.UUID, subject string, groups []string) ([]models.ComponentRoleBinding, error) {
	var bindings []models.ComponentRoleBinding
	conds := r.DB.Where("user_id = ?", userID) // V1 legacy rows
	if subject != "" {
		conds = conds.Or("subject_type = ? AND subject_id = ?", "user", subject)
	}
	if len(groups) > 0 {
		conds = conds.Or("subject_type = ? AND subject_id IN ?", "group", groups)
	}
	err := r.DB.Where("component_id = ?", componentID).Where(conds).
		Where("expires_at IS NULL OR expires_at > ?", time.Now()).
		Find(&bindings).Error
	return bindings, err
}
