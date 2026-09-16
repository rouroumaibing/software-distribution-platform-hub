package service

import (
	"github.com/google/uuid"

	"github.com/rouroumaibing/software-distribution-platform-hub/internal/common"
	"github.com/rouroumaibing/software-distribution-platform-hub/internal/run/models"
	"github.com/rouroumaibing/software-distribution-platform-hub/internal/run/repository"
)

// ReleaseService exposes CRUD over progressive-delivery history (RolloutRun
// rows) so the console's release view can list / inspect / roll back published
// versions instead of aggregating from runs on the frontend (P0-1).
type ReleaseService struct {
	repo *repository.RolloutRunRepository
}

func NewReleaseService(repo *repository.RolloutRunRepository) *ReleaseService {
	return &ReleaseService{repo: repo}
}

func (s *ReleaseService) Create(m *models.RolloutRun) error { return s.repo.Create(m) }

func (s *ReleaseService) Get(id uuid.UUID) (*models.RolloutRun, error) { return s.repo.GetByID(id) }

func (s *ReleaseService) List(p common.Pagination, pipelineRunID *uuid.UUID) ([]models.RolloutRun, int64, error) {
	return s.repo.List(p, pipelineRunID)
}

// Update applies the provided fields onto the existing row (preserving the FK
// and any fields the caller left out) and persists.
func (s *ReleaseService) Update(id uuid.UUID, m *models.RolloutRun) error {
	existing, err := s.repo.GetByID(id)
	if err != nil {
		return err
	}
	m.ID = existing.ID
	return s.repo.Update(m)
}

func (s *ReleaseService) Delete(id uuid.UUID) error { return s.repo.Delete(id) }
