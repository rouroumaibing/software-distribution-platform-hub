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

	// --- ui-12 §6.2 制品安全与合规字段（S-2 裁定：字段 + scan-result 双通道） ---
	// ScanStatus: unscanned（默认）| scanning | scanned。扫描器经
	// POST /artifacts/:id/scan-result 回写；没有内置扫描器，状态即事实。
	ScanStatus string `gorm:"size:16;not null;default:unscanned" json:"scanStatus"`
	// Signed / Signature: 签名状态与签名者标识（如 cosign key ref）。
	Signed    bool   `gorm:"not null;default:false" json:"signed"`
	Signature string `gorm:"size:256" json:"signature,omitempty"`
	// SbomSummary 是 SBOM 摘要（格式 + 包数等），完整 SBOM 走对象存储，不在行内。
	SbomSummary string `gorm:"type:text" json:"sbomSummary,omitempty"`
	// 漏洞严重级别计数（扫描器聚合值，不展开明细行——明细属扫描器域）。
	SevCritical int `gorm:"not null;default:0" json:"sevCritical"`
	SevHigh     int `gorm:"not null;default:0" json:"sevHigh"`
	SevMedium   int `gorm:"not null;default:0" json:"sevMedium"`
	SevLow      int `gorm:"not null;default:0" json:"sevLow"`
}

// ScanStatus values for Artifact.ScanStatus.
const (
	ScanUnscanned = "unscanned"
	ScanScanning  = "scanning"
	ScanScanned   = "scanned"
)

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
