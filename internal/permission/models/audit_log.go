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
	// 反规范化归属（ui-12 5.4 组件级审计下沉）：写行时由 audit 中间件经
	// Locator 把 resourceID 映射到所属组件（components 直取 / pipelines、
	// runs 回查）；映射不到留空 —— 组件管理员按本组件过滤查询时只见归属行。
	ComponentID *uuid.UUID `gorm:"type:uuid;index" json:"componentId,omitempty"`
	Detail      string     `gorm:"type:text" json:"detail,omitempty"`
	// Reason / Ticket are structured high-risk-operation context (ui-12 §4.4,
	// N-22): why the action was taken and the linked change ticket. They are
	// set by handlers via middleware.SetAuditReason for high-risk actions
	// (approval decisions, config rollback, credential rotation, canary
	// promote/rollback) and are queryable via GET /audit-logs.
	Reason string `gorm:"size:512" json:"reason,omitempty"`
	Ticket string `gorm:"size:64" json:"ticket,omitempty"`
}

func (AuditLog) TableName() string { return "audit_log" }
