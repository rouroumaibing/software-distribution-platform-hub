package service

import (
	"net/http"

	"github.com/google/uuid"

	"github.com/rouroumaibing/software-distribution-platform-hub/internal/common"
	"github.com/rouroumaibing/software-distribution-platform-hub/internal/pipeline/models"
)

// Parent-liveness guard 的 404（ERR.08404001 / ERR.08404002）。软删的 pipeline
// 与"从来不存在"对调用方是同一件事——聚合根已经没了，所以共用 404。
var (
	errPipelineNotFound = common.DomainError(common.KindPipeline, http.StatusNotFound, 1, "pipeline not found")
	errStageNotFound    = common.DomainError(common.KindPipeline, http.StatusNotFound, 2, "stage not found")
)

// StageStore 是 StageService 需要的 pipeline_stages 持久化面。抽成接口（而非直接
// 依赖具体的 *repository.StageRepository）的理由与 run 包的 PipelineRunStore 相同：
// 让父存在性校验能脱离 Postgres 做单测。
type StageStore interface {
	Create(*models.PipelineStage) error
	GetByID(id uuid.UUID) (*models.PipelineStage, error)
	Update(*models.PipelineStage) error
	Delete(id uuid.UUID) error
	ListByPipelineID(pipelineID uuid.UUID) ([]models.PipelineStage, error)
}

// PipelineParent 是 StageService 需要的 PipelineRepository 切片（做父存在性校验用）。
type PipelineParent interface {
	GetByID(uuid.UUID) (*models.Pipeline, error)
}

type StageService struct {
	repo      StageStore
	pipelines PipelineParent
}

func NewStageService(repo StageStore, pipelines PipelineParent) *StageService {
	return &StageService{repo: repo, pipelines: pipelines}
}

// ensureLivePipeline 是父存在性校验。stage 不是独立实体，而是 pipeline 聚合内的
// 部件：DDL 上的 on delete cascade（migrations/0001_init_schema.sql）**因为 pipeline
// 是软删（common/base.go）而永远不触发**，于是缺了这道校验时，一条已软删的
// pipeline 会继续回答两个端点——
//   - GET  /pipelines/:id/stages 照样列出阶段（读侧"孤儿可见"固定通道）；
//   - POST /pipelines/:id/stages 甚至能在已删 pipeline 下新建阶段（父行物理还在，
//     FK 校验通过）→ 这是"孤儿制造入口"，比读侧泄漏更严重。
//
// GetByID 走 GORM 的软删 scope，因此"软删"与"不存在"都收敛成
// gorm.ErrRecordNotFound，正好是我们要的语义（统一 404，而不是 200/500）。
func (s *StageService) ensureLivePipeline(pipelineID uuid.UUID) error {
	if _, err := s.pipelines.GetByID(pipelineID); err != nil {
		return errPipelineNotFound
	}
	return nil
}

// EnsureStageExists 给挂在 stage 下的资源（任务模板）用：stage 必须存在，且其
// pipeline 仍然存活。stage 自己没有 deleted_at（硬删），所以取不到即 404。
func (s *StageService) EnsureStageExists(stageID uuid.UUID) error {
	stage, err := s.repo.GetByID(stageID)
	if err != nil {
		return errStageNotFound
	}
	return s.ensureLivePipeline(stage.PipelineID)
}

func (s *StageService) Create(stage *models.PipelineStage) error {
	if err := s.ensureLivePipeline(stage.PipelineID); err != nil {
		return err
	}
	return s.repo.Create(stage)
}

// ListByPipeline returns the pipeline's stages ordered by Sequence.
func (s *StageService) ListByPipeline(pipelineID uuid.UUID) ([]models.PipelineStage, error) {
	if err := s.ensureLivePipeline(pipelineID); err != nil {
		return nil, err
	}
	return s.repo.ListByPipelineID(pipelineID)
}

// Update renames a stage and/or moves it in the sequence. Only Name and
// Sequence are mutable — PipelineID and audit columns stay untouched, and
// the task templates grouped under the stage are unaffected.
//
// 刻意不加父存在性校验：Update/Delete 不会制造孤儿，反而是在 pipeline 已软删后
// 清理残留结构的唯一通道；把入口关死会让孤儿行变成不可删的死数据。
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
