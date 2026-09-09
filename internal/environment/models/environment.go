package models

import (
	"github.com/google/uuid"

	"github.com/rouroumaibing/software-distribution-platform-hub/internal/common"
)

// Environment hangs off a Component (beta/alpha/gamma/prod) and pins it to
// a specific cluster + namespace.
type Environment struct {
	common.BaseNoSoftDelete
	ComponentID uuid.UUID `gorm:"type:uuid;not null;index" json:"componentId"`
	Key         string    `gorm:"size:64;not null" json:"key"`
	Name        string    `gorm:"size:128;not null" json:"name"`
	ClusterID   uuid.UUID `gorm:"type:uuid;not null;index" json:"clusterId"`

	// EnvType is "test" or "production"; used for platform-level policy
	// checks (e.g. mandatory approval before production deploys)
	// independent of what the pipeline author configured.
	EnvType string `gorm:"size:16;not null;default:test" json:"envType"`

	// Namespace convention: {org}-{component}-{env}.
	Namespace string `gorm:"size:128;not null" json:"namespace"`
}

func (Environment) TableName() string { return "environments" }

const (
	EnvTypeTest       = "test"
	EnvTypeProduction = "production"
)
