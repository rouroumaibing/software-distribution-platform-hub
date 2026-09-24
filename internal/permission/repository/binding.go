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

// ListMatching returns every ComponentRoleBinding on a component that applies
// to the subject: a direct `user` binding on the subject's `sub`, plus any
// `group` binding whose group the subject actually belongs to (so an empty
// groups slice simply skips the group branch).
//
// There is no local-user fallback. Before D3 the query started from
// `user_id = <local users.id>` and OR'ed the subject branch on top, so rows
// written before the §7 subject migration still resolved. D3 dropped that
// column (and the `users` table it pointed at), which leaves exactly one
// identity key — the Keycloak `sub` (§5.3) — and the DryRun regression test
// now asserts the emitted SQL has no `user_id` predicate at all.
//
// Expired grants (expires_at in the past) are excluded for the same reason as
// in PlatformRoleBindingRepository.ListMatching: this is the single path the
// authorization middleware resolves bindings through, so filtering anywhere
// else would leave an expired grant authorizing requests.
func (r *BindingRepository) ListMatching(componentID uuid.UUID, subject string, groups []string) ([]models.ComponentRoleBinding, error) {
	var bindings []models.ComponentRoleBinding
	subjectConds := r.DB.Where("subject_type = ? AND subject_id = ?", "user", subject)
	if len(groups) > 0 {
		subjectConds = subjectConds.Or("subject_type = ? AND subject_id IN ?", "group", groups)
	}
	err := r.DB.Where("component_id = ?", componentID).Where(subjectConds).
		Where("expires_at IS NULL OR expires_at > ?", time.Now()).
		Find(&bindings).Error
	return bindings, err
}
