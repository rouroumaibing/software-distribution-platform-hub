package models

import (
	"github.com/google/uuid"
)

// RoleAPIMapping is the authoritative, reviewable registry of "which API
// actions a given role grants" (ACCOUNT-PERMISSION-MODEL §5.1③). The role
// tables already carry an `actions` JSON column, but a table makes the
// role→interface mapping independently auditable and keeps it out of route
// registration statements. The binding service merges this table's rows with
// the role's `actions` JSON so the two never diverge in effect; SyncFromRoles
// keeps this table in lock-step with the role definitions.
type RoleAPIMapping struct {
	ID          uuid.UUID `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"id"`
	RoleKind    string    `gorm:"size:16;not null;index" json:"roleKind"` // platform | component
	RoleID      uuid.UUID `gorm:"type:uuid;not null;index" json:"roleId"`
	Action      string    `gorm:"size:64;not null" json:"action"`
	Description string    `json:"description,omitempty"`
}

func (RoleAPIMapping) TableName() string { return "role_api_mappings" }
