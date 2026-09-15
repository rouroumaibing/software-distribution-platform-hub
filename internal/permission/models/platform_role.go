package models

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"
)

// PlatformRole is a platform-level role granting console page / global
// actions (see DATA-MODEL §7.2).
//
// OrgID == nil  → built-in / global role (e.g. seeded sdp-admin).
// OrgID != nil  → org-scoped custom role (multi-org tenancy).
// This replaces V1's single `roles` table for platform-level grants; V1
// `Role` is kept until the P3 migration switches Enforcement over.
type PlatformRole struct {
	ID          uuid.UUID      `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"id"`
	OrgID       *uuid.UUID     `gorm:"type:uuid;index" json:"orgId,omitempty"`
	Name        string         `gorm:"size:64;not null" json:"name"`
	Description string         `json:"description,omitempty"`
	Actions     datatypes.JSON `gorm:"not null;default:'[]'" json:"actions"`
	IsSystem    bool           `gorm:"not null;default:false" json:"isSystem"`
	CreatedAt   time.Time      `json:"createdAt"`
}

func (PlatformRole) TableName() string { return "platform_roles" }
