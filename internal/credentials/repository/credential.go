package repository

import (
	"gorm.io/gorm"

	"github.com/rouroumaibing/software-distribution-platform-hub/internal/common"
	"github.com/rouroumaibing/software-distribution-platform-hub/internal/credentials/models"
)

type CredentialRepository struct {
	*common.Repository[models.Credential]
}

func NewCredentialRepository(db *gorm.DB) *CredentialRepository {
	return &CredentialRepository{common.NewRepository[models.Credential](db)}
}

// ListByScope filters credentials by scope (e.g. "environment") and an optional
// scope id. An empty scope returns all credentials.
func (r *CredentialRepository) ListByScope(scope, scopeID string, p common.Pagination) ([]models.Credential, int64, error) {
	return r.List(p, func(db *gorm.DB) *gorm.DB {
		q := db
		if scope != "" {
			q = q.Where("scope = ?", scope)
		}
		if scopeID != "" {
			q = q.Where("scope_id = ?", scopeID)
		}
		return q
	})
}
