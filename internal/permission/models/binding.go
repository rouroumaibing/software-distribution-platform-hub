package models

import (
	"time"

	"github.com/google/uuid"
)

// ComponentRoleBinding grants a role to a subject on one Component.
// No binding means no access — there's no implicit default.
//
// Multi-org: OrgID is denormalized (resolved from Component → Service →
// ServiceTree → Org at create time) so RBAC lookups can be isolated per
// org without a join.
//
// §7 subject model (authoritative as of P3):
//   - SubjectType/SubjectID : subject (user|group).
//   - ComponentRoleID       : the (component_roles) role granted.
// V1 legacy columns UserID/RoleID are kept nullable for the migration
// window so existing per-user bindings (roles table) still resolve; they
// are no longer written going forward. migration 0005 drops their NOT
// NULL and adds component_role_id (see DATA-MODEL §7.3).
type ComponentRoleBinding struct {
	ID              uuid.UUID  `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"id"`
	ComponentID     uuid.UUID  `gorm:"type:uuid;not null;index" json:"componentId"`
	OrgID           *uuid.UUID `gorm:"type:uuid;index" json:"orgId,omitempty"`
	UserID          *uuid.UUID `gorm:"type:uuid;index" json:"userId,omitempty"`    // V1 legacy (nullable post-P3)
	SubjectType     string     `gorm:"size:16" json:"subjectType,omitempty"`       // user|group
	SubjectID       string     `gorm:"size:128" json:"subjectId,omitempty"`        // user id (text) | group name
	RoleID          *uuid.UUID `gorm:"type:uuid" json:"roleId,omitempty"`           // V1 legacy role (nullable post-P3)
	ComponentRoleID *uuid.UUID `gorm:"type:uuid;index" json:"componentRoleId,omitempty"` // §7 role
	GrantedBy       *uuid.UUID `gorm:"type:uuid" json:"grantedBy,omitempty"`
	GrantedAt       time.Time  `gorm:"not null;default:now()" json:"grantedAt"`
}

func (ComponentRoleBinding) TableName() string { return "component_role_bindings" }
