package repository

import (
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/rouroumaibing/software-distribution-platform-hub/internal/component/models"
)

// ConfigSnapshotRepository is the append-only store for whole-environment
// config versions (N-18/N-19). Mirrors the pipeline version repository's
// contract: monotonic per (component, scope), append-only, rollback creates a
// NEW version instead of mutating history.
type ConfigSnapshotRepository struct {
	db *gorm.DB
}

func NewConfigSnapshotRepository(db *gorm.DB) *ConfigSnapshotRepository {
	return &ConfigSnapshotRepository{db: db}
}

// LatestVersion returns the highest version captured for the scope, or 0 when
// the scope has no snapshot yet.
func (r *ConfigSnapshotRepository) LatestVersion(componentID uuid.UUID, scopeKey string) (int, error) {
	var row struct{ Version int }
	err := r.db.Model(&models.ConfigSnapshot{}).
		Select("version").
		Where("component_id = ? AND scope_key = ?", componentID, scopeKey).
		Order("version DESC").
		First(&row).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return 0, nil
		}
		return 0, err
	}
	return row.Version, nil
}

// Create appends one snapshot row. The (component, scope, version) unique
// index is the last line of defense against concurrent double-append.
func (r *ConfigSnapshotRepository) Create(s *models.ConfigSnapshot) error {
	return r.db.Create(s).Error
}

// List returns the scope's versions, newest first. Rows are returned without
// the snapshot body loaded into memory consumers don't need it; GORM still
// selects it — the column is small (one row per key set) so no deferred
// loading is wired.
func (r *ConfigSnapshotRepository) List(componentID uuid.UUID, scopeKey string, limit int) ([]models.ConfigSnapshot, error) {
	var rows []models.ConfigSnapshot
	q := r.db.Where("component_id = ? AND scope_key = ?", componentID, scopeKey).
		Order("version DESC")
	if limit > 0 {
		q = q.Limit(limit)
	}
	err := q.Find(&rows).Error
	return rows, err
}

// GetByVersion resolves one snapshot of the scope by version.
func (r *ConfigSnapshotRepository) GetByVersion(componentID uuid.UUID, scopeKey string, version int) (*models.ConfigSnapshot, error) {
	var row models.ConfigSnapshot
	err := r.db.Where("component_id = ? AND scope_key = ? AND version = ?", componentID, scopeKey, version).
		First(&row).Error
	if err != nil {
		return nil, err
	}
	return &row, nil
}

// Tx exposes the raw handle so the rollback can run config upserts + deletes
// + the new snapshot inside one transaction.
func (r *ConfigSnapshotRepository) Tx() *gorm.DB { return r.db }

// LockLatest serializes version allocation across hub replicas: the caller
// selects the latest row FOR UPDATE before computing next = latest+1. On an
// empty scope there is nothing to lock — the unique index still guards the
// first concurrent append (one wins, the other retries/fails).
func (r *ConfigSnapshotRepository) LockLatest(tx *gorm.DB, componentID uuid.UUID, scopeKey string) (int, error) {
	var rows []models.ConfigSnapshot
	err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("component_id = ? AND scope_key = ?", componentID, scopeKey).
		Order("version DESC").Limit(1).Find(&rows).Error
	if err != nil {
		return 0, err
	}
	if len(rows) == 0 {
		return 0, nil
	}
	return rows[0].Version, nil
}
