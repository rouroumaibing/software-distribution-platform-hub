package models

import (
	"time"

	"github.com/google/uuid"
)

// PipelineApproval is the §7 approval record (richer than V1 Approval):
// run-scoped, component-scoped, and org-scoped, with requested_by /
// approver separation to support self-review prevention and audit
// (DATA-MODEL §7.4). V1's Approval table is kept until P2 migrates the
// approval subsystem over to this model.
type PipelineApproval struct {
	ID          uuid.UUID  `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"id"`
	OrgID       uuid.UUID  `gorm:"type:uuid;not null;index" json:"orgId"`
	RunID       uuid.UUID  `gorm:"type:uuid;not null;index" json:"runId"`
	TaskRunID   *uuid.UUID `gorm:"type:uuid;index" json:"taskRunId,omitempty"`
	ComponentID uuid.UUID  `gorm:"type:uuid;not null;index" json:"componentId"`
	// PipelineID 支撑审批中心行直达运行详情（run 详情路由挂在 pipeline 下）。
	PipelineID  uuid.UUID `gorm:"type:uuid;index" json:"pipelineId,omitempty"`
	Status      string    `gorm:"size:16;not null" json:"status"` // Pending|Approved|Rejected|Cancelled
	RequestedBy string    `gorm:"size:128;not null" json:"requestedBy"`
	Approver    string    `gorm:"size:128" json:"approver,omitempty"`
	// Assignee 是策略指派的**待决人列表**（JSON 数组字符串，如
	// `["alice","bob"]`）；空 = 任意持有 approval:approve 权限者皆可决。
	// 审批中心 `assignee=me` 视图按该字段过滤（ui-12 §5.1）。
	Assignee        string     `gorm:"size:512;not null;default:''" json:"assignee,omitempty"`
	DecisionComment string     `gorm:"size:1024" json:"decisionComment,omitempty"`
	CreatedAt       time.Time  `gorm:"not null;default:now()" json:"createdAt"`
	DecidedAt       *time.Time `gorm:"index" json:"decidedAt,omitempty"`
}

func (PipelineApproval) TableName() string { return "pipeline_approvals" }

// ApprovalPolicy configures how an Approval-type task is decided
// (ui-12 §5.1 / 决策 #4). Rows are component-scoped; EnvironmentID nil is
// the component-wide default, a set value overrides for that environment.
type ApprovalPolicy struct {
	ID            uuid.UUID  `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"id"`
	ComponentID   uuid.UUID  `gorm:"type:uuid;not null;index" json:"componentId"`
	EnvironmentID *uuid.UUID `gorm:"type:uuid;index" json:"environmentId,omitempty"`

	// Mode: any（任一人通过）| all（全部指派人都须通过）| n（至少 MinApprovals 人）。
	Mode         string `gorm:"size:8;not null;default:any" json:"mode"`
	MinApprovals int    `gorm:"not null;default:1" json:"minApprovals,omitempty"`
	// Assignees JSON 数组（`["alice","bob"]`）；空 = 不限审批人。
	Assignees      string `gorm:"size:512;not null;default:''" json:"assignees,omitempty"`
	TimeoutSeconds int    `gorm:"not null;default:0" json:"timeoutSeconds,omitempty"` // 0 = 沿用任务级时限

	CreatedBy string    `gorm:"size:128" json:"createdBy,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

func (ApprovalPolicy) TableName() string { return "approval_policies" }

// ValidApprovalMode guards the mode enum.
func ValidApprovalMode(m string) bool { return m == "any" || m == "all" || m == "n" }
