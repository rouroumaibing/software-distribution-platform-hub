package repository

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/rouroumaibing/software-distribution-platform-hub/internal/permission/models"
)

// PlatformRoleBindingRepository links subjects (user|group) to platform
// roles. ListMatching resolves every binding that applies to a subject,
// scoped to an org (nil org_id = global role bindings), merging user and
// group subjects like ComponentRoleBinding.ListMatching.
//
// Expiry (ACCOUNT-PERMISSION-MODEL §7.4): ListMatching only returns grants
// that are permanent or not yet expired. Expired rows are kept (not deleted)
// so an audit can still see what was granted; the periodic reaper may
// physical-delete them later. Filtering here — rather than only at the API
// edge — is what makes expires_at load-bearing: every permission decision
// resolves through ListMatching, so a filter anywhere else would leave an
// expired grant still authorizing requests.
type PlatformRoleBindingRepository struct{ DB *gorm.DB }

func NewPlatformRoleBindingRepository(db *gorm.DB) *PlatformRoleBindingRepository {
	return &PlatformRoleBindingRepository{DB: db}
}

// withActiveGrant narrows a query to grants still in force (permanent or not
// yet expired). now is passed in rather than read from the DB clock so the
// generated SQL is deterministic and can be asserted without a live server.
func withActiveGrant(q *gorm.DB, now time.Time) *gorm.DB {
	return q.Where("expires_at IS NULL OR expires_at > ?", now)
}

// ListMatching returns the platform-role bindings applying to (orgID,
// subject, groups). Global bindings (org_id IS NULL) are always included;
// org-scoped bindings only when orgID is non-nil and matches. Expired grants
// are excluded.
//
// The user branch matches on subject_id = subject (the Keycloak `sub`), per
// ACCOUNT-PERMISSION-MODEL §5.3 — the platform bindings table has no legacy
// uuid column, so there is no fallback branch to keep.
func (r *PlatformRoleBindingRepository) ListMatching(orgID *uuid.UUID, subject string, groups []string) ([]models.PlatformRoleBinding, error) {
	var bindings []models.PlatformRoleBinding
	conds := r.DB.Where("subject_type = ? AND subject_id = ?", "user", subject)
	if len(groups) > 0 {
		conds = conds.Or("subject_type = ? AND subject_id IN ?", "group", groups)
	}
	q := r.DB.Where(conds)
	if orgID != nil {
		q = q.Where("org_id IS NULL OR org_id = ?", *orgID)
	} else {
		q = q.Where("org_id IS NULL")
	}
	q = withActiveGrant(q, time.Now())
	err := q.Find(&bindings).Error
	return bindings, err
}

// List returns bindings, optionally narrowed by org and/or subject. It does
// NOT filter by expiry: the management UI has to show expired grants so an
// admin can see (and clean up) what lapsed.
func (r *PlatformRoleBindingRepository) List(orgID *uuid.UUID, subjectType, subjectID string) ([]models.PlatformRoleBinding, error) {
	var items []models.PlatformRoleBinding
	q := r.DB.Model(&models.PlatformRoleBinding{})
	if orgID != nil {
		q = q.Where("org_id = ?", *orgID)
	}
	if subjectType != "" {
		q = q.Where("subject_type = ?", subjectType)
	}
	if subjectID != "" {
		q = q.Where("subject_id = ?", subjectID)
	}
	err := q.Order("created_at DESC").Find(&items).Error
	return items, err
}

func (r *PlatformRoleBindingRepository) GetByID(id uuid.UUID) (*models.PlatformRoleBinding, error) {
	var item models.PlatformRoleBinding
	if err := r.DB.First(&item, id).Error; err != nil {
		return nil, err
	}
	return &item, nil
}

func (r *PlatformRoleBindingRepository) Create(item *models.PlatformRoleBinding) error {
	return r.DB.Create(item).Error
}

func (r *PlatformRoleBindingRepository) Delete(id uuid.UUID) error {
	return r.DB.Delete(&models.PlatformRoleBinding{}, "id = ?", id).Error
}

// DeleteExpired physically removes platform grants whose expires_at is in the
// past — the periodic counterpart to ListMatching's filter (§7.4). now is
// passed in for deterministic SQL in tests.
func (r *PlatformRoleBindingRepository) DeleteExpired(now time.Time) (int64, error) {
	res := expiredScope(r.DB, now).Delete(&models.PlatformRoleBinding{})
	return res.RowsAffected, res.Error
}

// ExistsActive reports whether an equivalent grant that is still in force
// already exists, so the service can answer 409 instead of silently stacking
// rows that resolve to the same actions.
func (r *PlatformRoleBindingRepository) ExistsActive(orgID *uuid.UUID, subjectType, subjectID string, roleID uuid.UUID) (bool, error) {
	q := r.DB.Model(&models.PlatformRoleBinding{}).
		Where("subject_type = ? AND subject_id = ? AND platform_role_id = ?", subjectType, subjectID, roleID)
	if orgID != nil {
		q = q.Where("org_id = ?", *orgID)
	} else {
		q = q.Where("org_id IS NULL")
	}
	q = withActiveGrant(q, time.Now())
	var n int64
	if err := q.Count(&n).Error; err != nil {
		return false, err
	}
	return n > 0, nil
}
