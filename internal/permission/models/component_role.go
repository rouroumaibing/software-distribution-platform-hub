package models

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"
)

// ComponentRole is a component-scoped role (ArgoCD Project-scoped RBAC,
// see DATA-MODEL §7.3).
//
// OrgID == nil  → built-in (component-viewer / component-editor /
//
//	component-approver, seeded).
//
// OrgID != nil  → org-defined custom role.
type ComponentRole struct {
	ID          uuid.UUID      `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"id"`
	OrgID       *uuid.UUID     `gorm:"type:uuid;index" json:"orgId,omitempty"`
	Name        string         `gorm:"size:64;not null" json:"name"`
	Description string         `json:"description,omitempty"`
	Actions     datatypes.JSON `gorm:"not null;default:'[]'" json:"actions"`
	IsSystem    bool           `gorm:"not null;default:false" json:"isSystem"`
	CreatedAt   time.Time      `json:"createdAt"`
}

func (ComponentRole) TableName() string { return "component_roles" }
