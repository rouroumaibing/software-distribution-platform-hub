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

// §7 action vocabulary (Casbin-style resource:action). These are what the
// enforcement middleware actually checks as of P3, and what component_roles
// / platform_roles seed (see cmd/hub/conf/09_rbac_multiorg.sql). V1 Role
// permissions (view/edit/create/delete/manage_permissions) are retained only
// for legacy bindings during the migration window.
const (
	ActionComponentRead    = "component:read"
	ActionComponentUpdate  = "component:update"
	ActionComponentDelete  = "component:delete"
	ActionComponentCreate  = "component:create"
	ActionComponentManage  = "component:manage"
	ActionPipelineRead     = "pipeline:read"
	ActionPipelineTrigger  = "pipeline:trigger"
	ActionPipelineCreate   = "pipeline:create"
	ActionPipelineUpdate   = "pipeline:update"
	ActionPipelineDelete   = "pipeline:delete"
	ActionApprovalApprove = "approval:approve"
	ActionConfigRead      = "config:read"
	ActionConfigUpdate    = "config:update"
	ActionArtifactRead    = "artifact:read"
	ActionArtifactDownload = "artifact:download"

	ActionPageOverviewView = "page:overview:view"
	ActionPageServicetreeView = "page:servicetree:view"
	ActionPageSettingsView = "page:settings:view"
	ActionOrgManage        = "org:manage"
	ActionUserManage       = "user:manage"
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
