package repository

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/rouroumaibing/software-distribution-platform-hub/internal/permission/models"
)

// PATRepository persists personal access tokens (§6.4).
type PATRepository struct{ db *gorm.DB }

func NewPATRepository(db *gorm.DB) *PATRepository { return &PATRepository{db: db} }

func (r *PATRepository) Create(t *models.PersonalAccessToken) error { return r.db.Create(t).Error }

func (r *PATRepository) FindByHash(hash string) (*models.PersonalAccessToken, error) {
	var out models.PersonalAccessToken
	if err := r.db.Where("token_hash = ?", hash).First(&out).Error; err != nil {
		return nil, err
	}
	return &out, nil
}

func (r *PATRepository) ListBySubject(subject string) ([]models.PersonalAccessToken, error) {
	var out []models.PersonalAccessToken
	err := r.db.Where("subject = ? AND revoked_at IS NULL", subject).
		Order("created_at desc").Find(&out).Error
	return out, err
}

func (r *PATRepository) Revoke(subject string, id uuid.UUID, at time.Time) error {
	return r.db.Model(&models.PersonalAccessToken{}).
		Where("id = ? AND subject = ? AND revoked_at IS NULL", id, subject).
		Update("revoked_at", at).Error
}

func (r *PATRepository) TouchLastUsed(id uuid.UUID, at time.Time) {
	_ = r.db.Model(&models.PersonalAccessToken{}).Where("id = ?", id).
		Update("last_used_at", at).Error
}
