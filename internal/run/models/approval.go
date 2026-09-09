package models

import (
	"time"

	"github.com/google/uuid"
)

// Approval is one decision (approve/reject) against an Approval-type
// TaskRun; RequiredApprovals on the template may need several rows.
type Approval struct {
	ID        uuid.UUID `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"id"`
	TaskRunID uuid.UUID `gorm:"type:uuid;not null;index" json:"taskRunId"`
	Approver  string    `gorm:"size:128;not null" json:"approver"`
	Decision  string    `gorm:"size:16;not null" json:"decision"` // approved/rejected
	Comment   string    `json:"comment,omitempty"`
	DecidedAt time.Time `gorm:"not null;default:now()" json:"decidedAt"`
}

func (Approval) TableName() string { return "approvals" }
