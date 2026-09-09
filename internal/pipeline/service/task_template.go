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
}

func NewTaskTemplateService(repo *repository.TaskTemplateRepository) *TaskTemplateService {
	return &TaskTemplateService{repo: repo}
}

func (s *TaskTemplateService) Create(t *models.PipelineTaskTemplate) error { return s.repo.Create(t) }

func (s *TaskTemplateService) ListByStage(stageID uuid.UUID) ([]models.PipelineTaskTemplate, error) {
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

func (s *TaskTemplateService) Delete(id uuid.UUID) error { return s.repo.Delete(id) }
