package models

import (
	"time"

	"github.com/google/uuid"
)

// Artifact is a permanent, versioned, user-browsable build deliverable —
// distinct from PipelineTaskTemplate.Produces/Consumes, which are
// ephemeral hand-offs between stages scoped to a single PipelineRun.
type Artifact struct {
	ID            uuid.UUID  `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"id"`
	ComponentID   uuid.UUID  `gorm:"type:uuid;not null;index" json:"componentId"`
	PipelineRunID *uuid.UUID `gorm:"type:uuid" json:"pipelineRunId,omitempty"`
	TaskRunID     *uuid.UUID `gorm:"type:uuid" json:"taskRunId,omitempty"`

	Version      string `gorm:"size:128;not null" json:"version"`
	ArtifactType string `gorm:"size:32;not null;default:generic" json:"artifactType"` // image/binary/archive/generic

	StorageKey string `gorm:"size:512;not null" json:"storageKey"`
	SizeBytes  *int64 `json:"sizeBytes,omitempty"`
	Checksum   string `gorm:"size:128" json:"checksum,omitempty"`
	CommitSHA  string `gorm:"size:64" json:"commitSha,omitempty"`

	ExpiresAt *time.Time `json:"expiresAt,omitempty"`
	CreatedAt time.Time  `json:"createdAt"`
}

func (Artifact) TableName() string { return "artifacts" }
