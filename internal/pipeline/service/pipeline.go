package service

import (
	"fmt"
	"net/http"

	"github.com/google/uuid"

	"github.com/rouroumaibing/software-distribution-platform-hub/internal/common"
	"github.com/rouroumaibing/software-distribution-platform-hub/internal/pipeline/models"
)

// activePhases are run phases that must block a delete (functional safety,
// DELETE-CONTRACT §2.1 / §6.4 #6): deleting while a run is in flight would
// orphan an in-progress publish. Historical runs (Succeeded/Failed/Cancelled)
// are allowed and simply retain their audit trail.
var activePhases = []string{"Pending", "Running", "WaitingApproval"}

// PipelineRunExistence reports whether a pipeline still has run history, so
// Delete can refuse to drop a definition whose runs must outlive it.
type PipelineRunExistence interface {
	CountByPipeline(pipelineID uuid.UUID) (int64, error)
	CountActiveByPipeline(pipelineID uuid.UUID, phases []string) (int64, error)
}

// PipelineStore is the persistence surface PipelineService needs. Narrow
// interfaces (same pattern as StageStore / ComponentStore / ServiceStore) keep
// the delete guard unit-testable without Postgres;
// *repository.PipelineRepository satisfies it as-is.
type PipelineStore interface {
	Create(*models.Pipeline) error
	GetByID(uuid.UUID) (*models.Pipeline, error)
	Update(*models.Pipeline) error
	Delete(uuid.UUID) error
	FindByComponentID(uuid.UUID, common.Pagination) ([]models.Pipeline, int64, error)
	FindAll(uuid.UUID, string, string, common.Pagination) ([]models.Pipeline, int64, error)
}

// PipelineService manages Pipeline's own metadata (name/kind/description);
// stages and task templates are managed via StageService/TaskTemplateService
// as their own sub-resources, mirroring how ComponentConfig sits alongside
// Component.
type PipelineService struct {
	repo     PipelineStore
	versions VersionPublisher
	runRepo  PipelineRunExistence
}

// NewPipelineService takes a VersionPublisher rather than a bare version repo.
// 早前这里收的是 `VersionStore`（只有 Create），配一个**没人调用**的
// `PublishVersion` 占位方法 —— 于是"结构改动要记版本"这件事在代码里根本没有
// 落点。改为 receiver 接口后，PipelineService 只能**委托**发布，无法自行拼出
// 一个半截版本；快照组装与版本号自增都归 PipelineVersionService 一家。
func NewPipelineService(repo PipelineStore, versions VersionPublisher, runRepo PipelineRunExistence) *PipelineService {
	return &PipelineService{repo: repo, versions: versions, runRepo: runRepo}
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

// Update copies only the editable scalar fields onto the existing row,
// preserving immutable identity (ID), audit (CreatedAt, CreatedBy) and the
// owning ComponentID, then republishes the definition snapshot.
//
// 版本号的自增已交给发布方（PipelineVersionService.Publish 是唯一的自增点），
// 从而"版本号变了"与"有对应快照"不可能再各走各的。未装配发布方的退化场景
// （测试 / 最小化装配）仍然原地自增，保持既有行为不静默丢失。
func (s *PipelineService) Update(id uuid.UUID, p *models.Pipeline) error {
	existing, err := s.repo.GetByID(id)
	if err != nil {
		return err
	}
	existing.Name = p.Name
	existing.Kind = p.Kind
	existing.Description = p.Description
	if s.versions != nil {
		if err := s.repo.Update(existing); err != nil {
			return err
		}
		return s.versions.Publish(id, p.CreatedBy)
	}
	existing.Version = existing.Version + 1
	return s.repo.Update(existing)
}

// Delete refuses to drop a pipeline that still has an in-flight run. Historical
// runs (Succeeded/Failed/Cancelled) are allowed: they must outlive the
// definition but do not block deletion (DELETE-CONTRACT §2.1 / §6.4 #9). The
// rejection carries a structured reasons list so the console can render exactly
// what is still in progress.
func (s *PipelineService) Delete(id uuid.UUID) error {
	n, err := s.runRepo.CountActiveByPipeline(id, activePhases)
	if err != nil {
		return err
	}
	if n > 0 {
		return common.DomainErrorWithReasons(common.KindPipeline, http.StatusConflict, 1,
			"pipeline still has in-progress runs; terminate them before deleting",
			fmt.Sprintf("%d 条运行仍在进行中（Pending/Running/WaitingApproval），请先终止后再删除", n))
	}
	return s.repo.Delete(id)
}
