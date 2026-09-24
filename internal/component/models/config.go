package models

import (
	"time"

	"github.com/google/uuid"
)

// ComponentConfig is a single key/value parameter for a component. A row
// with EnvironmentID == nil is the global default; a row with it set
// overrides the default for just that environment.
type ComponentConfig struct {
	ID            uuid.UUID  `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"id"`
	ComponentID   uuid.UUID  `gorm:"type:uuid;not null;index" json:"componentId"`
	EnvironmentID *uuid.UUID `gorm:"type:uuid" json:"environmentId,omitempty"`

	Key string `gorm:"size:128;not null" json:"key"`

	Value     string `gorm:"type:text" json:"value,omitempty"`
	IsSecret  bool   `gorm:"not null;default:false" json:"isSecret"`
	SecretRef string `gorm:"size:128" json:"secretRef,omitempty"`

	Description string `json:"description"`
	// CreatedBy / UpdatedBy carry the author's RBAC subject (the Keycloak
	// `sub`, §5.3) rather than a local user id: hub keeps no user table (D3),
	// so there is no local id left to record. Both columns were `uuid` until
	// migration 0015 converted them to text.
	CreatedBy *string   `gorm:"size:128" json:"createdBy,omitempty"`
	UpdatedBy *string   `gorm:"size:128" json:"updatedBy,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

func (ComponentConfig) TableName() string { return "component_configs" }

// ComponentConfigHistory is an append-only audit trail; a row is written
// on every create/update/delete against ComponentConfig.
type ComponentConfigHistory struct {
	ID            uuid.UUID  `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"id"`
	ComponentID   uuid.UUID  `gorm:"type:uuid;not null;index" json:"componentId"`
	EnvironmentID *uuid.UUID `gorm:"type:uuid" json:"environmentId,omitempty"`
	// EnvironmentKey is a denormalized snapshot of environments.key taken at
	// write time. The FK on environment_id was deliberately dropped
	// (DELETE-CONTRACT §6.6-2 / B-14) so that deleting an environment no
	// longer 500s on existing history rows, and so a deleted environment's
	// audit rows stay readable — "alpha（已删除）" instead of a dangling uuid.
	EnvironmentKey string `gorm:"size:64" json:"environmentKey,omitempty"`
	Key            string `gorm:"size:128;not null" json:"key"`
	Action         string `gorm:"size:16;not null" json:"action"`
	OldValue       string `gorm:"type:text" json:"oldValue,omitempty"`
	NewValue       string `gorm:"type:text" json:"newValue,omitempty"`
	// ChangedBy is the author's RBAC subject (§5.3) — text since 0015, for the
	// same reason as ComponentConfig.CreatedBy above.
	ChangedBy *string   `gorm:"size:128" json:"changedBy,omitempty"`
	ChangedAt time.Time `gorm:"not null;default:now()" json:"changedAt"`
}

func (ComponentConfigHistory) TableName() string { return "component_config_history" }
