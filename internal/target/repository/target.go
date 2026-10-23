package repository

import (
	"errors"
	"time"

	"gorm.io/gorm"

	"github.com/rouroumaibing/software-distribution-platform-hub/internal/common"
	"github.com/rouroumaibing/software-distribution-platform-hub/internal/target/models"
)

// ErrEnrollTokenInvalid reports a one-time enroll token that cannot be
// consumed: unknown, expired, already burned, or (when a name assertion was
// supplied) bound to a differently named target. The gateway maps all of
// these to one 401 — no oracle about which check failed.
var ErrEnrollTokenInvalid = errors.New("enroll token invalid or already used")

type TargetRepository struct {
	*common.Repository[models.Target]
}

func NewTargetRepository(db *gorm.DB) *TargetRepository {
	return &TargetRepository{common.NewRepository[models.Target](db)}
}

// GetByName resolves a target by its unique Name (the identifier a Runner
// presents as X-Target-Name when it dials the gateway).
func (r *TargetRepository) GetByName(name string) (*models.Target, error) {
	var tg models.Target
	if err := r.DB.First(&tg, "name = ?", name).Error; err != nil {
		return nil, err
	}
	return &tg, nil
}

// BurnEnrollToken atomically consumes a one-time enroll token and stores the
// per-target credential in the same conditional UPDATE, so two dials racing
// on the same token cannot both win (single-use is enforced by the database,
// not by check-then-write). expectName, when non-empty, joins the WHERE as a
// name assertion: a token presented under a foreign X-Target-Name burns
// nothing and fails. Returns the post-burn row.
func (r *TargetRepository) BurnEnrollToken(token, credential, expectName string) (*models.Target, error) {
	var tg models.Target
	err := r.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.First(&tg, "enroll_token = ?", token).Error; err != nil {
			return err
		}
		q := tx.Model(&models.Target{}).
			Where("id = ? AND enroll_token = ? AND enroll_token_expire_at > ?", tg.ID, token, time.Now())
		if expectName != "" {
			q = q.Where("name = ?", expectName)
		}
		res := q.Updates(map[string]any{
			"enroll_token":           nil,
			"enroll_token_expire_at": nil,
			"runner_credential":      credential,
		})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return ErrEnrollTokenInvalid
		}
		return tx.First(&tg, "id = ?", tg.ID).Error
	})
	if err != nil {
		return nil, err
	}
	return &tg, nil
}

// GetByCredential resolves a target by its per-target gateway credential —
// the identity source for per-target WS dials (X-Target-Name is only a
// consistency assertion at the caller).
func (r *TargetRepository) GetByCredential(cred string) (*models.Target, error) {
	var tg models.Target
	if err := r.DB.First(&tg, "runner_credential = ?", cred).Error; err != nil {
		return nil, err
	}
	return &tg, nil
}
