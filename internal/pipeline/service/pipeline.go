package service

import (
	"net/http"

	"github.com/google/uuid"

	"github.com/rouroumaibing/software-distribution-platform-hub/internal/common"
	"github.com/rouroumaibing/software-distribution-platform-hub/internal/pipeline/models"
	"github.com/rouroumaibing/software-distribution-platform-hub/internal/pipeline/repository"
)

// PipelineRunExistence reports whether a pipeline still has run history, so
// Delete can refuse to drop a definition whose runs must outlive it.
type PipelineRunExistence interface {
	CountByPipeline(pipelineID uuid.UUID) (int64, error)
}

// PipelineService manages Pipeline's own metadata (name/kind/description);
// stages and task templates are managed via StageService/TaskTemplateService
// as their own sub-resources, mirroring how ComponentConfig sits alongside
// Component.
type PipelineService struct {
	repo        *repository.PipelineRepository
	versionRepo *repository.VersionRepository
	runRepo     PipelineRunExistence
}

func NewPipelineService(repo *repository.PipelineRepository, versionRepo *repository.VersionRepository, runRepo PipelineRunExistence) *PipelineService {
	return &PipelineService{repo: repo, versionRepo: versionRepo, runRepo: runRepo}
}

func (s *PipelineService) Create(p *models.Pipeline) error {
	p.Version = 1
	return s.repo.Create(p)
}

func (s *PipelineService) Get(id uuid.UUID) (*models.Pipeline, error) { return s.repo.GetByID(id) }

func (s *PipelineService) ListByComponent(componentID uuid.UUID, p common.Pagination) ([]models.Pipeline, int64, error) {
	return s.repo.FindByComponentID(componentID, p)
}

// PipelineListOpts carries the optional filters for the global pipeline list.
// An empty value on any field means "no filter on this dimension".
type PipelineListOpts struct {
	ComponentID uuid.UUID
	Kind        string
	Name        string
}

// List returns the cross-component pipeline list (global /pipelines endpoint),
// supporting the filters the console's run center and future ⌘K search need.
func (s *PipelineService) List(opts PipelineListOpts, p common.Pagination) ([]models.Pipeline, int64, error) {
	return s.repo.FindAll(opts.ComponentID, opts.Kind, opts.Name, p)
}

// Update bumps Version and copies only the editable scalar fields onto the
// existing row, preserving immutable identity (ID), audit (CreatedAt,
// CreatedBy) and the owning ComponentID. Any structural edit (stages/tasks
// changed via StageService/TaskTemplateService) should call PublishVersion
// afterwards to snapshot the new state; historical PipelineRuns keep pointing
// at the version they were triggered with.
func (s *PipelineService) Update(id uuid.UUID, p *models.Pipeline) error {
	existing, err := s.repo.GetByID(id)
	if err != nil {
		return err
	}
	existing.Name = p.Name
	existing.Kind = p.Kind
	existing.Description = p.Description
	existing.Version = existing.Version + 1
	return s.repo.Update(existing)
}

// Delete refuses to drop a pipeline that still has run history. Runs are the
// long-term source of truth and must outlive the definition they were built
// from, so deleting while runs exist would orphan that history.
func (s *PipelineService) Delete(id uuid.UUID) error {
	n, err := s.runRepo.CountByPipeline(id)
	if err != nil {
		return err
	}
	if n > 0 {
		return common.DomainError(common.KindPipeline, http.StatusConflict, 1, "pipeline still has run history; cannot delete")
	}
	return s.repo.Delete(id)
}

// PublishVersion snapshots the pipeline's current stages+tasks as a new
// PipelineVersion row. Called after any stage/task-template mutation.
// TODO: 具体的 snapshot 组装(读取全部 stages+tasks 序列化成 JSON)留待
// StageService/TaskTemplateService 落地后再补,这里先占位。
func (s *PipelineService) PublishVersion(pipelineID uuid.UUID, snapshot []byte, createdBy string) error {
	p, err := s.repo.GetByID(pipelineID)
	if err != nil {
		return err
	}
	return s.versionRepo.Create(&models.PipelineVersion{
		PipelineID: pipelineID,
		Version:    p.Version,
		Snapshot:   snapshot,
		CreatedBy:  createdBy,
	})
}
