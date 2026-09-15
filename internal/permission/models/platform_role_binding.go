package models

import (
	"time"

	"github.com/google/uuid"
)

// PlatformRoleBinding links a subject (user or Keycloak group) to a
// PlatformRole. OrgID is denormalized for multi-org isolation of the
// lookup (nil = global role binding).
type PlatformRoleBinding struct {
	ID             uuid.UUID  `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"id"`
	OrgID          *uuid.UUID `gorm:"type:uuid;index" json:"orgId,omitempty"`
	SubjectType    string     `gorm:"size:16;not null" json:"subjectType"` // user|group
	SubjectID      string     `gorm:"size:128;not null" json:"subjectId"`
	PlatformRoleID uuid.UUID  `gorm:"type:uuid;not null" json:"platformRoleId"`
	CreatedAt      time.Time  `gorm:"not null;default:now()" json:"createdAt"`
}

func (PlatformRoleBinding) TableName() string { return "platform_role_bindings" }
