package service

import (
	"github.com/google/uuid"

	"github.com/rouroumaibing/software-distribution-platform-hub/internal/pipeline/models"
	"github.com/rouroumaibing/software-distribution-platform-hub/internal/pipeline/repository"
)

type StageService struct{ repo *repository.StageRepository }

func NewStageService(repo *repository.StageRepository) *StageService {
	return &StageService{repo: repo}
}

func (s *StageService) Create(stage *models.PipelineStage) error { return s.repo.Create(stage) }

// ListByPipeline returns the pipeline's stages ordered by Sequence.
func (s *StageService) ListByPipeline(pipelineID uuid.UUID) ([]models.PipelineStage, error) {
	return s.repo.ListByPipelineID(pipelineID)
}

// Update renames a stage and/or moves it in the sequence. Only Name and
// Sequence are mutable — PipelineID and audit columns stay untouched, and
// the task templates grouped under the stage are unaffected.
func (s *StageService) Update(id uuid.UUID, in *models.PipelineStage) (*models.PipelineStage, error) {
	existing, err := s.repo.GetByID(id)
	if err != nil {
		return nil, err
	}
	existing.Name = in.Name
	existing.Sequence = in.Sequence
	if err := s.repo.Update(existing); err != nil {
		return nil, err
	}
	return existing, nil
}

func (s *StageService) Delete(id uuid.UUID) error { return s.repo.Delete(id) }
