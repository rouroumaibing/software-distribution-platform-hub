package service

import (
	"github.com/google/uuid"

	"github.com/rouroumaibing/software-distribution-platform-hub/internal/common"
	"github.com/rouroumaibing/software-distribution-platform-hub/internal/component/models"
	"github.com/rouroumaibing/software-distribution-platform-hub/internal/component/repository"
)

type ComponentConfigService struct {
	repo *repository.ComponentConfigRepository
}

func NewComponentConfigService(repo *repository.ComponentConfigRepository) *ComponentConfigService {
	return &ComponentConfigService{repo: repo}
}

func (s *ComponentConfigService) List(componentID uuid.UUID, environmentID *uuid.UUID, p common.Pagination) ([]models.ComponentConfig, int64, error) {
	return s.repo.FindByComponentID(componentID, environmentID, p)
}

// GetByKey resolves the config row for a (component, key) pair; pass a nil
// environmentID to match the global default row.
func (s *ComponentConfigService) GetByKey(componentID uuid.UUID, key string, environmentID *uuid.UUID) (*models.ComponentConfig, error) {
	return s.repo.GetByKey(componentID, key, environmentID)
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
		ComponentID:   cfg.ComponentID,
		EnvironmentID: cfg.EnvironmentID,
		Key:           cfg.Key,
		Action:        action,
		NewValue:      newValue,
		ChangedBy:     changedBy,
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
		ComponentID:   old.ComponentID,
		EnvironmentID: old.EnvironmentID,
		Key:           old.Key,
		Action:        "delete",
		OldValue:      oldValue,
		ChangedBy:     changedBy,
	})
}
