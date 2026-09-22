package service

import (
	"errors"

	"github.com/google/uuid"
	"gorm.io/datatypes"
	"gorm.io/gorm"

	applog "github.com/rouroumaibing/software-distribution-platform-hub/internal/common/logger"
	"github.com/rouroumaibing/software-distribution-platform-hub/internal/pipeline/models"
	"github.com/rouroumaibing/software-distribution-platform-hub/internal/pipeline/repository"
)

type TaskTemplateService struct {
	repo *repository.TaskTemplateRepository
	// stages 提供 EnsureStageExists：任务模板挂在 stage 上，stage 又挂在
	// pipeline 上，所以新建/列举之前要确认整条父链都还在（见 stage.go）。
	stages *StageService
	// versions 是可选的结构变更钩子（C-09）：模板增删改后重新发布定义快照。
	versions VersionPublisher
}

func NewTaskTemplateService(repo *repository.TaskTemplateRepository, stages *StageService) *TaskTemplateService {
	return &TaskTemplateService{repo: repo, stages: stages}
}

// SetVersionPublisher 装配版本快照钩子（main.go 调用）。setter 形式与
// StageService 一致，避免改动既有构造签名。
func (s *TaskTemplateService) SetVersionPublisher(p VersionPublisher) { s.versions = p }

// publishVersionForStage 由 stage 反查 pipeline 后发布快照。尽力而为 —— 理由与
// StageService.publishVersion 相同（结构已落库，把留档失败报成 API 错误等于撒谎）。
func (s *TaskTemplateService) publishVersionForStage(stageID uuid.UUID) {
	if s.versions == nil || s.stages == nil {
		return
	}
	pipelineID, err := s.stages.PipelineIDOf(stageID)
	if err != nil || pipelineID == uuid.Nil {
		return
	}
	if perr := s.versions.Publish(pipelineID, ""); perr != nil {
		applog.Warnf("pipeline: version snapshot failed after task edit pipeline=%s err=%v", pipelineID, perr)
	}
}

// Create 先校验父链存活：否则对一个不存在的 stageId 会撞 FK 报错塌成 500，
// 对已软删 pipeline 下的 stage 则会造出新的孤儿行。
func (s *TaskTemplateService) Create(t *models.PipelineTaskTemplate) error {
	if err := s.stages.EnsureStageExists(t.StageID); err != nil {
		return err
	}
	if err := s.repo.Create(t); err != nil {
		return err
	}
	s.publishVersionForStage(t.StageID)
	return nil
}

func (s *TaskTemplateService) ListByStage(stageID uuid.UUID) ([]models.PipelineTaskTemplate, error) {
	if err := s.stages.EnsureStageExists(stageID); err != nil {
		return nil, err
	}
	return s.repo.ListByStageID(stageID)
}

// Update 走的是 Save 全字段更新，而前端任务编辑器只提交它管理的字段
// （Build 任务不会带 scriptArgs / retryPolicy / stageId）。这里：
//  1. 未提交的 NOT NULL JSON 列用库里的现值补齐，避免写 NULL 触发 23502；
//  2. stage_id 不允许经 body 换绑（与 stage.Update 的 PipelineID 同一约定），
//     否则编辑一次任务就会把 stage_id 写成全零 UUID、任务从阶段上脱钩。
func (s *TaskTemplateService) Update(id uuid.UUID, t *models.PipelineTaskTemplate) error {
	t.ID = id

	var cur models.PipelineTaskTemplate
	err := s.repo.DB.First(&cur, "id = ?", id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	if err != nil {
		return err
	}

	t.StageID = cur.StageID
	keep := func(dst *datatypes.JSON, curVal datatypes.JSON, def string) {
		if len(*dst) == 0 {
			if len(curVal) > 0 {
				*dst = curVal
			} else {
				*dst = datatypes.JSON(def)
			}
		}
	}
	keep(&t.ScriptArgs, cur.ScriptArgs, "[]")
	keep(&t.Command, cur.Command, "[]")
	keep(&t.Args, cur.Args, "[]")
	keep(&t.Produces, cur.Produces, "[]")
	keep(&t.Consumes, cur.Consumes, "[]")
	keep(&t.RetryPolicy, cur.RetryPolicy, `{"maxRetries":0}`)

	if err := s.repo.Update(t); err != nil {
		return err
	}
	s.publishVersionForStage(cur.StageID)
	return nil
}

// Delete 同 stage.Delete：不加父存在性校验，保证已软删 pipeline 下残留的任务
// 模板仍能通过 API 清掉。
func (s *TaskTemplateService) Delete(id uuid.UUID) error {
	// 先读 stageID：删除后（软删 scope 生效）就读不到了，而版本发布需要它。
	var stageID uuid.UUID
	if cur, err := s.repo.GetByID(id); err == nil {
		stageID = cur.StageID
	}
	if err := s.repo.Delete(id); err != nil {
		return err
	}
	s.publishVersionForStage(stageID)
	return nil
}
