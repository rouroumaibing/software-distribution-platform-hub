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

	// CleanupState makes a failed object removal a **durable, retryable state**
	// instead of a log line (backlog B-16 收口). Values: CleanupStateActive
	// (normal) / CleanupStatePendingDeletion (expired, object removal still
	// outstanding — the row is kept only so the GC can retry).
	CleanupState string    `gorm:"size:32;not null;default:active" json:"cleanupState,omitempty"`
	CreatedAt    time.Time `json:"createdAt"`
}

// CleanupState values. Kept as constants (not bare strings) so the GC query and
// the listing filter cannot drift apart silently.
const (
	// CleanupStateActive: row and object are both live. The column default.
	CleanupStateActive = "active"
	// CleanupStatePendingDeletion: the artifact is expired and the row is
	// retained solely so the next GC pass can retry deleting the object. Such
	// rows are hidden from component listings — they are logically deleted, and
	// offering a download the GC is actively removing would be a lie.
	CleanupStatePendingDeletion = "pending_deletion"
)

func (Artifact) TableName() string { return "artifacts" }
