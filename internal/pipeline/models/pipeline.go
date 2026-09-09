package models

import (
	"github.com/google/uuid"

	"github.com/rouroumaibing/software-distribution-platform-hub/internal/common"
)

// Pipeline is a definition-level template (e.g. "构建流水线","发布流水线").
type Pipeline struct {
	common.Base
	ComponentID uuid.UUID `gorm:"type:uuid;not null;index" json:"componentId"`
	Name        string    `gorm:"size:128;not null" json:"name"`
	Kind        string    `gorm:"size:32;not null;default:custom" json:"kind"` // build/release/custom
	Description string    `json:"description"`
	CreatedBy   string    `gorm:"size:128" json:"createdBy,omitempty"`

	// Version increments on every structural edit; a PipelineRun records
	// which version it was triggered with.
	Version int `gorm:"not null;default:1" json:"version"`
}

func (Pipeline) TableName() string { return "pipelines" }
