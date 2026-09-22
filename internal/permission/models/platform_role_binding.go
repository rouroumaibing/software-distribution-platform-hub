package models

import (
	"time"

	"github.com/google/uuid"
)

// PlatformRoleBinding links a subject (user or Keycloak group) to a
// PlatformRole. OrgID is denormalized for multi-org isolation of the
// lookup (nil = global role binding).
// ExpiresAt carries the grant's TTL (ACCOUNT-PERMISSION-MODEL §7.4 / §10 #15).
// NULL = permanent. A non-NULL value in the past means the grant is already
// dead: ListMatching filters it out, and the periodic reaper (§7.4) may
// physical-delete it, so an expired row is never honoured as a permission
// source. Without this filter `expires_at` would be purely decorative.
type PlatformRoleBinding struct {
	ID             uuid.UUID  `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"id"`
	OrgID          *uuid.UUID `gorm:"type:uuid;index" json:"orgId,omitempty"`
	SubjectType    string     `gorm:"size:16;not null;index:idx_platform_role_bindings_subject,priority:1" json:"subjectType"` // user|group
	SubjectID      string     `gorm:"size:128;not null;index:idx_platform_role_bindings_subject,priority:2" json:"subjectId"`
	PlatformRoleID uuid.UUID  `gorm:"type:uuid;not null" json:"platformRoleId"`
	ExpiresAt      *time.Time `gorm:"index" json:"expiresAt,omitempty"`
	CreatedAt      time.Time  `gorm:"not null;default:now()" json:"createdAt"`
}

func (PlatformRoleBinding) TableName() string { return "platform_role_bindings" }
