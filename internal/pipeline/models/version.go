package models

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"
)

// PipelineVersion is an immutable snapshot of a pipeline's stages+tasks,
// captured every time a structural edit is saved. Mirrors what the hub
// materializes into runnerapi.PipelineRunSpec.Tasks[] at trigger time.
type PipelineVersion struct {
	ID         uuid.UUID      `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"id"`
	PipelineID uuid.UUID      `gorm:"type:uuid;not null;index" json:"pipelineId"`
	Version    int            `gorm:"not null" json:"version"`
	Snapshot   datatypes.JSON `gorm:"not null" json:"snapshot"`
	CreatedBy  string         `gorm:"size:128" json:"createdBy,omitempty"`
	CreatedAt  time.Time      `json:"createdAt"`
}

func (PipelineVersion) TableName() string { return "pipeline_versions" }
