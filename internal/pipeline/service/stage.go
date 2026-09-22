package service

import (
	"net/http"
	"strings"

	"github.com/google/uuid"

	"github.com/rouroumaibing/software-distribution-platform-hub/internal/common"
	applog "github.com/rouroumaibing/software-distribution-platform-hub/internal/common/logger"
	"github.com/rouroumaibing/software-distribution-platform-hub/internal/pipeline/models"
)

// Parent-liveness guard 的 404（ERR.08404001 / ERR.08404002）。软删的 pipeline
// 与"从来不存在"对调用方是同一件事——聚合根已经没了，所以共用 404。
var (
	errPipelineNotFound = common.DomainError(common.KindPipeline, http.StatusNotFound, 1, "pipeline not found")
	errStageNotFound    = common.DomainError(common.KindPipeline, http.StatusNotFound, 2, "stage not found")
	// errBadExecutionMode 是 400 而非 404/409：调用方把枚举写错了，不是资源状态问题。
	// 没走"静默落默认值"是因为静默会掩盖客户端的契约错误 —— 传了非法值却拿到 200，
	// 调用方会以为自己设置生效了。
	errBadExecutionMode = common.DomainError(common.KindPipeline, http.StatusBadRequest, 1,
		"invalid executionMode (want parallel|serial)")
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

// TemplateCascade 让 stage 软删时把其下任务模板一并软删。DDL 上
// pipeline_task_templates.stage_id 是 `on delete cascade`，但那条级联只在**硬删**
// 时触发 —— 两个模型都改为软删（common.Base）后它就成了死条款。缺了这一步会留下
// "挂到已删 stage 上的活模板"：API 读不到（EnsureStageExists 返 404），行却是活的，
// 且恢复语义断掉（stage 恢复回来、模板状态不一致）。
//
// 可选依赖：单测/不需要模板级联的场景可传 nil。
type TemplateCascade interface {
	DeleteByStageID(stageID uuid.UUID) error
}

type StageService struct {
	repo      StageStore
	pipelines PipelineParent
	templates TemplateCascade
	// versions 是可选的结构变更钩子。装配后，每次阶段增删改都会重新发布一次
	// 定义快照（C-09）。nil 时静默跳过 —— 单测与最小化装配不需要它。
	versions VersionPublisher
}

func NewStageService(repo StageStore, pipelines PipelineParent, templates TemplateCascade) *StageService {
	return &StageService{repo: repo, pipelines: pipelines, templates: templates}
}

// SetVersionPublisher 装配版本快照钩子（main.go 调用）。用 setter 而不是构造参数，
// 是为了不破坏既有 NewStageService(repo, parent, cascade) 的调用点与单测。
func (s *StageService) SetVersionPublisher(p VersionPublisher) { s.versions = p }

// publishVersion 是**尽力而为**的留档：结构改动已经落库，此时把发布失败当成
// API 错误返回就是在对调用方撒谎（它会以为改动没生效并重试，而重试只会再写一次）。
// 失败记结构化日志，并由"快照记的是**整份定义**"这一性质自愈 —— 下一次成功保存
// 会把累积状态整体记下来，不会永久缺失。
//
// 与 audit 中间件 / object 清理的失败处理同一口径（best-effort + 可观测）。
func (s *StageService) publishVersion(pipelineID uuid.UUID) {
	if s.versions == nil || pipelineID == uuid.Nil {
		return
	}
	if err := s.versions.Publish(pipelineID, ""); err != nil {
		applog.Warnf("pipeline: version snapshot failed after stage edit pipeline=%s err=%v", pipelineID, err)
	}
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
// pipeline 仍然存活。stage 自己也是软删（common.Base），于是 GetByID 的软删 scope
// 会把"已删 stage"与"不存在 stage"一起收敛成 404 —— 正是我们要的语义，也让
// "已删 stage 下的模板"不会通过 API 泄漏（DELETE-CONTRACT §6.6-3(a)）。
func (s *StageService) EnsureStageExists(stageID uuid.UUID) error {
	stage, err := s.repo.GetByID(stageID)
	if err != nil {
		return errStageNotFound
	}
	return s.ensureLivePipeline(stage.PipelineID)
}

// normalizeExecutionMode 把外部取值收敛成规范小写。容忍大小写与首尾空白，是因为
// 权威文档之间曾经不一致：hub/API-REFERENCE.md 写 `parallel | serial`（小写），而
// hub/DATA-MODEL.md §6.4-① 的草案 ALTER 写 'Parallel'（首字母大写）。客户端的
// "P" 不该被判 400；存下来的一律是规范小写，DB 里不出现两种拼写。
// 空串返回空串 —— "没传"由调用方决定语义（Create 落默认 / Update 视为不改）。
func normalizeExecutionMode(m string) string {
	return strings.ToLower(strings.TrimSpace(m))
}

func (s *StageService) Create(stage *models.PipelineStage) error {
	if err := s.ensureLivePipeline(stage.PipelineID); err != nil {
		return err
	}
	// 缺省即 parallel（与 DDL 的 default 一致）：不传 executionMode 的老客户端行为
	// 不变，不会因为这次加字段而拿到一个空字符串模式。
	mode := normalizeExecutionMode(stage.ExecutionMode)
	if mode == "" {
		mode = models.ExecutionModeParallel
	}
	if !models.IsValidExecutionMode(mode) {
		return errBadExecutionMode
	}
	stage.ExecutionMode = mode
	if err := s.repo.Create(stage); err != nil {
		return err
	}
	s.publishVersion(stage.PipelineID)
	return nil
}

// PipelineIDOf 返回某阶段所属流水线。任务模板需要它来触发版本发布
// （task → stage → pipeline 的父链）。
func (s *StageService) PipelineIDOf(stageID uuid.UUID) (uuid.UUID, error) {
	stage, err := s.repo.GetByID(stageID)
	if err != nil {
		return uuid.Nil, errStageNotFound
	}
	return stage.PipelineID, nil
}

// ListByPipeline returns the pipeline's stages ordered by Sequence.
func (s *StageService) ListByPipeline(pipelineID uuid.UUID) ([]models.PipelineStage, error) {
	if err := s.ensureLivePipeline(pipelineID); err != nil {
		return nil, err
	}
	return s.repo.ListByPipelineID(pipelineID)
}

// Update renames a stage, moves it in the sequence, and/or flips its
// executionMode. Only Name / Sequence / ExecutionMode are mutable — PipelineID
// and audit columns stay untouched, and the task templates grouped under the
// stage are unaffected.
//
// 刻意不加父存在性校验：Update/Delete 不会制造孤儿，反而是在 pipeline 已软删后
// 清理残留结构的唯一通道；把入口关死会让孤儿行变成不可删的死数据。
//
// ExecutionMode 是**局部更新**语义（与 Name/Sequence 的"整体覆盖"不同）：空串 =
// 不改。因为 Update 同时承担"改名"与"重排"两个动作，而 console 的每一处结构操作
// 都只关心其中一个字段 —— 若空串也照写，一次「重排 sequence」的 PUT 就会把阶段
// 的串行模式静默重置回默认值。非空但非法 → 400（不静默纠正）。
func (s *StageService) Update(id uuid.UUID, in *models.PipelineStage) (*models.PipelineStage, error) {
	existing, err := s.repo.GetByID(id)
	if err != nil {
		return nil, err
	}
	existing.Name = in.Name
	existing.Sequence = in.Sequence
	if mode := normalizeExecutionMode(in.ExecutionMode); mode != "" {
		if !models.IsValidExecutionMode(mode) {
			return nil, errBadExecutionMode
		}
		existing.ExecutionMode = mode
	}
	if err := s.repo.Update(existing); err != nil {
		return nil, err
	}
	s.publishVersion(existing.PipelineID)
	return existing, nil
}

// Delete soft-deletes a stage and, in a service-level cascade, its task
// templates (see TemplateCascade: the DDL's on delete cascade no longer fires
// once both sides are soft-deleted).
//
// 模板先删、stage 后删：这样级联失败时 stage 仍然存活，整个删除是**可重试**的；
// 反之则会留下"stage 已删、模板仍是活行"的不一致状态。
func (s *StageService) Delete(id uuid.UUID) error {
	// 先读 pipelineID：删除后（软删 scope 生效）就再也读不到了，版本发布需要它。
	// 读不到不阻断删除 —— Delete 明确承担"清理已软删 pipeline 下的残留结构"，
	// 这种情况本就没有可发布的父实体。
	var pipelineID uuid.UUID
	if stage, err := s.repo.GetByID(id); err == nil {
		pipelineID = stage.PipelineID
	}
	if s.templates != nil {
		if err := s.templates.DeleteByStageID(id); err != nil {
			return err
		}
	}
	if err := s.repo.Delete(id); err != nil {
		return err
	}
	s.publishVersion(pipelineID)
	return nil
}
