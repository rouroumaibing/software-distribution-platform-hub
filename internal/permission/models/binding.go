package models

import (
	"time"

	"github.com/google/uuid"
)

// ComponentRoleBinding grants exactly one Role to one User on one
// Component. No binding means no access — there's no implicit default.
type ComponentRoleBinding struct {
	ID          uuid.UUID  `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"id"`
	ComponentID uuid.UUID  `gorm:"type:uuid;not null;index" json:"componentId"`
	UserID      uuid.UUID  `gorm:"type:uuid;not null;index" json:"userId"`
	RoleID      uuid.UUID  `gorm:"type:uuid;not null" json:"roleId"`
	GrantedBy   *uuid.UUID `gorm:"type:uuid" json:"grantedBy,omitempty"`
	GrantedAt   time.Time  `gorm:"not null;default:now()" json:"grantedAt"`
}

func (ComponentRoleBinding) TableName() string { return "component_role_bindings" }
