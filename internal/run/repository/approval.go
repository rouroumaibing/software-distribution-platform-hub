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

// ListCenter serves the approval center (ui-12 §5.1): pending-assigned-to-me /
// initiated-by-me / decided-by-me. `assignee` matches the JSON assignee list
// with an exact-quoted LIKE (subject 含引号不存在 —— 主体是 token sub)。
func (r *PipelineApprovalRepository) ListCenter(view, subject string, limit int) ([]models.PipelineApproval, error) {
	var items []models.PipelineApproval
	q := r.DB.Order("created_at desc")
	switch view {
	case "pending":
		q = q.Where("status = ?", "Pending")
		if subject != "" {
			// 空指派 = 任意审批人；非空指派按精确引号匹配成员。
			q = q.Where("assignee = '' OR assignee LIKE ?", `%"`+subject+`"%`)
		}
	case "initiated":
		q = q.Where("requested_by = ?", subject)
	case "decided":
		q = q.Where("approver = ? AND status <> ?", subject, "Pending")
	}
	if limit > 0 {
		q = q.Limit(limit)
	}
	err := q.Find(&items).Error
	return items, err
}

// ---- ApprovalPolicy（ui-12 §5.1 / 决策 #4）---------------------------------

// UpsertPolicy writes the policy for (component, env) — env NULL means the
// component-wide default. One row per scope key.
func (r *PipelineApprovalRepository) UpsertPolicy(p *models.ApprovalPolicy) error {
	var existing models.ApprovalPolicy
	q := r.DB.Where("component_id = ?", p.ComponentID)
	if p.EnvironmentID == nil {
		q = q.Where("environment_id IS NULL")
	} else {
		q = q.Where("environment_id = ?", *p.EnvironmentID)
	}
	err := q.First(&existing).Error
	if err == nil {
		existing.Mode = p.Mode
		existing.MinApprovals = p.MinApprovals
		existing.Assignees = p.Assignees
		existing.TimeoutSeconds = p.TimeoutSeconds
		return r.DB.Save(&existing).Error
	}
	if err != gorm.ErrRecordNotFound {
		return err
	}
	return r.DB.Create(p).Error
}

func (r *PipelineApprovalRepository) ListPolicies(componentID uuid.UUID) ([]models.ApprovalPolicy, error) {
	var items []models.ApprovalPolicy
	err := r.DB.Where("component_id = ?", componentID).Order("environment_id asc").Find(&items).Error
	return items, err
}

// PolicyFor resolves the effective policy for a component+environment:
// the environment override wins, else the component default, else nil.
func (r *PipelineApprovalRepository) PolicyFor(componentID, environmentID uuid.UUID) (*models.ApprovalPolicy, error) {
	var p models.ApprovalPolicy
	err := r.DB.Where("component_id = ? AND environment_id = ?", componentID, environmentID).First(&p).Error
	if err == nil {
		return &p, nil
	}
	if err != gorm.ErrRecordNotFound {
		return nil, err
	}
	err = r.DB.Where("component_id = ? AND environment_id IS NULL", componentID).First(&p).Error
	if err != nil {
		return nil, err
	}
	return &p, nil
}
