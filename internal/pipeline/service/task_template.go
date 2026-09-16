package service

import (
	"errors"

	"github.com/google/uuid"
	"gorm.io/datatypes"
	"gorm.io/gorm"

	"github.com/rouroumaibing/software-distribution-platform-hub/internal/pipeline/models"
	"github.com/rouroumaibing/software-distribution-platform-hub/internal/pipeline/repository"
)

type TaskTemplateService struct {
	repo *repository.TaskTemplateRepository
	// stages 提供 EnsureStageExists：任务模板挂在 stage 上，stage 又挂在
	// pipeline 上，所以新建/列举之前要确认整条父链都还在（见 stage.go）。
	stages *StageService
}

func NewTaskTemplateService(repo *repository.TaskTemplateRepository, stages *StageService) *TaskTemplateService {
	return &TaskTemplateService{repo: repo, stages: stages}
}

// Create 先校验父链存活：否则对一个不存在的 stageId 会撞 FK 报错塌成 500，
// 对已软删 pipeline 下的 stage 则会造出新的孤儿行。
func (s *TaskTemplateService) Create(t *models.PipelineTaskTemplate) error {
	if err := s.stages.EnsureStageExists(t.StageID); err != nil {
		return err
	}
	return s.repo.Create(t)
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

	return s.repo.Update(t)
}

// Delete 同 stage.Delete：不加父存在性校验，保证已软删 pipeline 下残留的任务
// 模板仍能通过 API 清掉。
func (s *TaskTemplateService) Delete(id uuid.UUID) error { return s.repo.Delete(id) }
