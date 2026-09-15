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
	ID              uuid.UUID  `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"id"`
	OrgID           uuid.UUID  `gorm:"type:uuid;not null;index" json:"orgId"`
	RunID           uuid.UUID  `gorm:"type:uuid;not null;index" json:"runId"`
	TaskRunID       *uuid.UUID `gorm:"type:uuid;index" json:"taskRunId,omitempty"`
	ComponentID     uuid.UUID  `gorm:"type:uuid;not null;index" json:"componentId"`
	Status          string     `gorm:"size:16;not null" json:"status"` // Pending|Approved|Rejected|Cancelled
	RequestedBy     string     `gorm:"size:128;not null" json:"requestedBy"`
	Approver        string     `gorm:"size:128" json:"approver,omitempty"`
	DecisionComment string     `gorm:"size:1024" json:"decisionComment,omitempty"`
	CreatedAt       time.Time  `gorm:"not null;default:now()" json:"createdAt"`
	DecidedAt       *time.Time `gorm:"index" json:"decidedAt,omitempty"`
}

func (PipelineApproval) TableName() string { return "pipeline_approvals" }
