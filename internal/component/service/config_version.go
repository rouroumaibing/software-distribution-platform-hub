package service

import (
	"fmt"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/rouroumaibing/software-distribution-platform-hub/internal/component/models"
	"github.com/rouroumaibing/software-distribution-platform-hub/internal/component/repository"
)

// ConfigSnapshotService versions the whole-environment config as immutable
// snapshots (ui-12 §4.1/§4.2, N-18/N-19): every config mutation captures a
// new version (deduplicated when the body is unchanged, mirroring
// PipelineVersion), diff compares two versions, and rollback materializes an
// old version as the current config and appends a NEW version — history is
// never rewritten.
type ConfigSnapshotService struct {
	repo    *repository.ConfigSnapshotRepository
	cfgRepo *repository.ComponentConfigRepository
}

func NewConfigSnapshotService(repo *repository.ConfigSnapshotRepository, cfgRepo *repository.ComponentConfigRepository) *ConfigSnapshotService {
	return &ConfigSnapshotService{repo: repo, cfgRepo: cfgRepo}
}

// Capture snapshots the current config rows of one scope as the next version.
// A no-change capture is skipped (same dedup rule as pipeline versions) so a
// UI save loop cannot flood the version list.
func (s *ConfigSnapshotService) Capture(componentID uuid.UUID, environmentID *uuid.UUID, comment string, by *string) error {
	rows, err := s.cfgRepo.ListAll(componentID, environmentID)
	if err != nil {
		return err
	}
	entries := models.BuildConfigSnapshot(rows)
	body, err := models.EncodeConfigSnapshot(entries)
	if err != nil {
		return err
	}
	scopeKey := models.ScopeKeyOf(environmentID)
	return s.repo.Tx().Transaction(func(tx *gorm.DB) error {
		latest, err := s.repo.LockLatest(tx, componentID, scopeKey)
		if err != nil {
			return err
		}
		if latest > 0 {
			prev, err := s.repo.GetByVersion(componentID, scopeKey, latest)
			if err == nil && string(prev.Snapshot) == string(body) {
				return nil // body unchanged → no new version
			}
		}
		return tx.Create(&models.ConfigSnapshot{
			ComponentID:   componentID,
			EnvironmentID: environmentID,
			ScopeKey:      scopeKey,
			Version:       latest + 1,
			Comment:       comment,
			Snapshot:      body,
			CreatedBy:     by,
		}).Error
	})
}

// List returns the scope's versions newest-first (meta rows; the console
// renders version/comment/author and only fetches bodies it opens).
func (s *ConfigSnapshotService) List(componentID uuid.UUID, environmentID *uuid.UUID, limit int) ([]models.ConfigSnapshot, error) {
	return s.repo.List(componentID, models.ScopeKeyOf(environmentID), limit)
}

// Get resolves one version with its (already secret-masked) entries.
func (s *ConfigSnapshotService) Get(componentID uuid.UUID, environmentID *uuid.UUID, version int) (*models.ConfigSnapshot, []models.ConfigSnapshotEntry, error) {
	row, err := s.repo.GetByVersion(componentID, models.ScopeKeyOf(environmentID), version)
	if err != nil {
		return nil, nil, err
	}
	entries, err := models.DecodeConfigSnapshot(row.Snapshot)
	if err != nil {
		return nil, nil, err
	}
	return row, entries, nil
}

// Diff compares two versions of the same scope.
func (s *ConfigSnapshotService) Diff(componentID uuid.UUID, environmentID *uuid.UUID, a, b int) (models.ConfigSnapshotDiff, error) {
	scopeKey := models.ScopeKeyOf(environmentID)
	from, err := s.repo.GetByVersion(componentID, scopeKey, a)
	if err != nil {
		return models.ConfigSnapshotDiff{}, fmt.Errorf("版本 v%d 不存在: %w", a, err)
	}
	to, err := s.repo.GetByVersion(componentID, scopeKey, b)
	if err != nil {
		return models.ConfigSnapshotDiff{}, fmt.Errorf("版本 v%d 不存在: %w", b, err)
	}
	fromEntries, err := models.DecodeConfigSnapshot(from.Snapshot)
	if err != nil {
		return models.ConfigSnapshotDiff{}, err
	}
	toEntries, err := models.DecodeConfigSnapshot(to.Snapshot)
	if err != nil {
		return models.ConfigSnapshotDiff{}, err
	}
	return models.DiffConfigSnapshots(fromEntries, toEntries, a, b, from.CreatedAt, to.CreatedAt), nil
}

// Rollback materializes version `version` as the current config and appends a
// NEW snapshot (comment carries the rollback reason) — history is append-only.
func (s *ConfigSnapshotService) Rollback(componentID uuid.UUID, environmentID *uuid.UUID, version int, comment string, by *string) (*models.ConfigSnapshot, error) {
	scopeKey := models.ScopeKeyOf(environmentID)
	target, err := s.repo.GetByVersion(componentID, scopeKey, version)
	if err != nil {
		return nil, fmt.Errorf("版本 v%d 不存在: %w", version, err)
	}
	entries, err := models.DecodeConfigSnapshot(target.Snapshot)
	if err != nil {
		return nil, err
	}

	var created models.ConfigSnapshot
	err = s.repo.Tx().Transaction(func(tx *gorm.DB) error {
		if err := applyEntries(tx, componentID, environmentID, entries, by); err != nil {
			return err
		}
		latest, err := s.repo.LockLatest(tx, componentID, scopeKey)
		if err != nil {
			return err
		}
		created = models.ConfigSnapshot{
			ComponentID:   componentID,
			EnvironmentID: environmentID,
			ScopeKey:      scopeKey,
			Version:       latest + 1,
			Comment:       fmt.Sprintf("回滚到 v%d：%s", version, comment),
			Snapshot:      target.Snapshot, // exact bytes of the rolled-back body
			CreatedBy:     by,
		}
		return tx.Create(&created).Error
	})
	if err != nil {
		return nil, err
	}
	return &created, nil
}

// applyEntries makes the live config rows match the snapshot body inside the
// caller's transaction: upsert every entry, delete keys absent from it.
func applyEntries(tx *gorm.DB, componentID uuid.UUID, environmentID *uuid.UUID, entries []models.ConfigSnapshotEntry, by *string) error {
	keep := make(map[string]bool, len(entries))
	for _, e := range entries {
		keep[e.Key] = true
		var existing models.ComponentConfig
		q := tx.Where("component_id = ? AND key = ?", componentID, e.Key)
		if environmentID != nil {
			q = q.Where("environment_id = ?", *environmentID)
		} else {
			q = q.Where("environment_id IS NULL")
		}
		err := q.First(&existing).Error
		switch {
		case err == gorm.ErrRecordNotFound:
			row := models.ComponentConfig{
				ComponentID:   componentID,
				EnvironmentID: environmentID,
				Key:           e.Key,
				Value:         e.Value,
				IsSecret:      e.IsSecret,
				SecretRef:     e.SecretRef,
				CreatedBy:     by,
				UpdatedBy:     by,
			}
			if e.IsSecret {
				// A rollback cannot resurrect plaintext the hub never stored;
				// route the key through the credential secret-ref flow.
				row.Value = ""
			}
			if err := tx.Create(&row).Error; err != nil {
				return err
			}
		case err != nil:
			return err
		default:
			existing.Value = e.Value
			existing.IsSecret = e.IsSecret
			existing.SecretRef = e.SecretRef
			existing.UpdatedBy = by
			if err := tx.Save(&existing).Error; err != nil {
				return err
			}
		}
	}
	var stale []models.ComponentConfig
	q := tx.Where("component_id = ?", componentID)
	if environmentID != nil {
		q = q.Where("environment_id = ?", *environmentID)
	} else {
		q = q.Where("environment_id IS NULL")
	}
	if err := q.Find(&stale).Error; err != nil {
		return err
	}
	for _, row := range stale {
		if !keep[row.Key] {
			if err := tx.Delete(&row).Error; err != nil {
				return err
			}
		}
	}
	return nil
}
