package repository

import (
	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/rouroumaibing/software-distribution-platform-hub/internal/run/models"
)

// PipelineApprovalRepository persists hub-side approval records
// (DATA-MODEL §7.4). The hub owns the audit trail + self-review gate;
// the runner still owns the DAG pause (it records status on the TaskRun).
type PipelineApprovalRepository struct{ DB *gorm.DB }

func NewPipelineApprovalRepository(db *gorm.DB) *PipelineApprovalRepository {
	return &PipelineApprovalRepository{DB: db}
}

func (r *PipelineApprovalRepository) Create(a *models.PipelineApproval) error {
	return r.DB.Create(a).Error
}

// GetByRunAndTask returns the single approval row for a DAG node, used by
// the approval gating flow to enforce the status machine + self-review.
func (r *PipelineApprovalRepository) GetByRunAndTask(runID, taskRunID uuid.UUID) (*models.PipelineApproval, error) {
	var a models.PipelineApproval
	err := r.DB.Where("run_id = ? AND task_run_id = ?", runID, taskRunID).First(&a).Error
	return &a, err
}

func (r *PipelineApprovalRepository) Update(a *models.PipelineApproval) error {
	return r.DB.Save(a).Error
}

func (r *PipelineApprovalRepository) ListByRun(runID uuid.UUID) ([]models.PipelineApproval, error) {
	var items []models.PipelineApproval
	err := r.DB.Where("run_id = ?", runID).Order("created_at asc").Find(&items).Error
	return items, err
}

// ListPending returns still-undecided approvals, oldest first, for the timeout
// sweeper (B-11 审批超时).
//
// 到期限值**不**下推成 SQL 谓词：每个审批任务各自声明
// `approval_config.timeoutSeconds`，而该值存在任务模板里，不在本表 —— 拿一个
// 统一的 cutoff 去过滤会漏掉"超时设得特别长"的行、误伤"设得很短"的行。
// 因此这里只按状态取，逐行取到各自的时限后由纯谓词判定。
func (r *PipelineApprovalRepository) ListPending(limit int) ([]models.PipelineApproval, error) {
	var items []models.PipelineApproval
	q := r.DB.Where("status = ?", "Pending").Order("created_at asc")
	if limit > 0 {
		q = q.Limit(limit)
	}
	err := q.Find(&items).Error
	return items, err
}
