package service

import (
	"github.com/google/uuid"

	"github.com/rouroumaibing/software-distribution-platform-hub/internal/common"
	applog "github.com/rouroumaibing/software-distribution-platform-hub/internal/common/logger"
	"github.com/rouroumaibing/software-distribution-platform-hub/internal/component/models"
	"github.com/rouroumaibing/software-distribution-platform-hub/internal/component/repository"
)

// EnvKeyResolver resolves an environment id to its human-readable key, so the
// config audit trail can carry a denormalized environment_key snapshot. The FK
// from component_config_history.environment_id to environments was dropped
// (B-14 / DELETE-CONTRACT §6.6-2) precisely so history outlives environment
// deletion; the snapshot is what keeps a deleted environment's rows readable.
//
// Deliberately a one-method interface returning a plain string: the component
// package must not import environment/models just to read a single column.
type EnvKeyResolver interface {
	ResolveKey(envID uuid.UUID) (string, error)
}

type ComponentConfigService struct {
	repo    *repository.ComponentConfigRepository
	envKeys EnvKeyResolver
}

func NewComponentConfigService(repo *repository.ComponentConfigRepository, envKeys EnvKeyResolver) *ComponentConfigService {
	return &ComponentConfigService{repo: repo, envKeys: envKeys}
}

func (s *ComponentConfigService) List(componentID uuid.UUID, environmentID *uuid.UUID, p common.Pagination) ([]models.ComponentConfig, int64, error) {
	return s.repo.FindByComponentID(componentID, environmentID, p)
}

// GetByKey resolves the config row for a (component, key) pair; pass a nil
// environmentID to match the global default row.
func (s *ComponentConfigService) GetByKey(componentID uuid.UUID, key string, environmentID *uuid.UUID) (*models.ComponentConfig, error) {
	return s.repo.GetByKey(componentID, key, environmentID)
}

// envKeySnapshot resolves the environment key stamped onto a history row. A nil
// environment (global default config) yields "". A resolution failure is logged
// and degrades to "" rather than blocking the config write — the audit row
// records what happened, it is not a correctness gate.
func (s *ComponentConfigService) envKeySnapshot(envID *uuid.UUID) string {
	if envID == nil || s.envKeys == nil {
		return ""
	}
	key, err := s.envKeys.ResolveKey(*envID)
	if err != nil {
		applog.Warnf("component config: cannot resolve environment key for %s: %v", envID, err)
		return ""
	}
	return key
}

// Upsert creates or updates a config key and writes an audit row. Secret
// values never get logged into history in plaintext — only the fact that
// a change happened.
func (s *ComponentConfigService) Upsert(cfg *models.ComponentConfig, changedBy *uuid.UUID) error {
	action := "update"
	if cfg.ID == uuid.Nil {
		action = "create"
	}
	if err := s.repo.Update(cfg); err != nil {
		return err
	}
	newValue := cfg.Value
	if cfg.IsSecret {
		newValue = "<secret>"
	}
	return s.repo.LogHistory(&models.ComponentConfigHistory{
		ComponentID:    cfg.ComponentID,
		EnvironmentID:  cfg.EnvironmentID,
		EnvironmentKey: s.envKeySnapshot(cfg.EnvironmentID),
		Key:            cfg.Key,
		Action:         action,
		NewValue:       newValue,
		ChangedBy:      changedBy,
	})
}

// Delete removes a config key and writes a delete row to the audit trail,
// capturing the last value as OldValue (masked when secret).
func (s *ComponentConfigService) Delete(id uuid.UUID, changedBy *uuid.UUID) error {
	var old *models.ComponentConfig
	old, err := s.repo.GetByID(id)
	if err != nil {
		return err
	}
	if err := s.repo.Delete(id); err != nil {
		return err
	}
	oldValue := old.Value
	if old.IsSecret {
		oldValue = "<secret>"
	}
	return s.repo.LogHistory(&models.ComponentConfigHistory{
		ComponentID:    old.ComponentID,
		EnvironmentID:  old.EnvironmentID,
		EnvironmentKey: s.envKeySnapshot(old.EnvironmentID),
		Key:            old.Key,
		Action:         "delete",
		OldValue:       oldValue,
		ChangedBy:      changedBy,
	})
}
