package models

import (
	"time"

	"github.com/google/uuid"
)

// AuditLog is the single audit sink for every mutating request
// (ACCOUNT-PERMISSION-MODEL §6). It is written by the audit middleware only —
// business handlers never call it directly. Failures to write are downgraded
// to a warning (they must not block the operation).
type AuditLog struct {
	ID           uuid.UUID `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"id"`
	Timestamp    time.Time `gorm:"not null;index" json:"timestamp"`
	SubjectType  string    `gorm:"size:16" json:"subjectType,omitempty"` // user | group
	Subject      string    `gorm:"size:128;index" json:"subject"`        // sub or /group
	Roles        string    `gorm:"size:512" json:"roles,omitempty"`      // comma-joined action set, for quick review
	SourceIP     string    `gorm:"size:64" json:"sourceIP,omitempty"`
	Action       string    `gorm:"size:64;index" json:"action"` // e.g. component:create
	ResourceType string    `gorm:"size:32" json:"resourceType,omitempty"`
	ResourceID   string    `gorm:"size:64;index" json:"resourceId,omitempty"`
	Result       string    `gorm:"size:16" json:"result"` // success | failure
	StatusCode   int       `json:"statusCode"`
	Detail       string    `gorm:"type:text" json:"detail,omitempty"`
}

func (AuditLog) TableName() string { return "audit_log" }
