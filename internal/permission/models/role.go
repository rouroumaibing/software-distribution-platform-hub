package models

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"
)

// Permission is one of the atomic actions a Role can grant.
const (
	PermissionView              = "view"
	PermissionEdit              = "edit"
	PermissionCreate            = "create"
	PermissionDelete            = "delete"
	PermissionManagePermissions = "manage_permissions"
)

// Role is either a system-wide built-in (OrgID == nil: Viewer/Editor/Admin)
// or an org-defined custom role. Managed via seed data / migration, not a
// full CRUD API — the console only lists them for the permission picker.
type Role struct {
	ID          uuid.UUID      `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"id"`
	OrgID       *uuid.UUID     `gorm:"type:uuid" json:"orgId,omitempty"`
	Name        string         `gorm:"size:64;not null" json:"name"`
	Permissions datatypes.JSON `gorm:"not null;default:'[]'" json:"permissions"`
	IsSystem    bool           `gorm:"not null;default:false" json:"isSystem"`
	CreatedAt   time.Time      `json:"createdAt"`
}

func (Role) TableName() string { return "roles" }
