package models

import (
	"github.com/google/uuid"
	"gorm.io/datatypes"

	"github.com/rouroumaibing/software-distribution-platform-hub/internal/common"

	// Reuse the runner's CRD enum types so a template's Type can be copied
	// straight into runnerapi.PipelineTaskSpec.Type at trigger time.
	runnerapi "github.com/rouroumaibing/software-distribution-platform-runner/api/v1alpha1"
)

// PipelineTaskTemplate is the definition-level counterpart of
// runnerapi.PipelineTaskSpec.
//
// It embeds common.BaseNoSoftDelete so id / created_at / updated_at are all
// mapped. Before this, the struct carried its own ID only, so the two
// timestamp columns existed in the table (0001_init_schema.sql) but were
// invisible to the API. BaseNoSoftDelete (not Base) because stages/templates
// have no deleted_at column — they are hard-deleted, unlike their soft-deleted
// parent pipeline.
type PipelineTaskTemplate struct {
	common.BaseNoSoftDelete
	StageID uuid.UUID `gorm:"type:uuid;not null;index" json:"stageId"`

	Name         string                     `gorm:"size:128;not null" json:"name"`
	Type         runnerapi.PipelineTaskType `gorm:"size:32;not null" json:"type"` // Build/Release/Approval
	DisplayOrder int                        `gorm:"not null;default:0" json:"displayOrder"`

	// Build tasks run an inline command (e.g. "pytest", "go build") inside
	// Image. Release tasks apply a chart/manifest (see ReleaseConfig). Script
	// execution (ScriptPath) remains supported as an optional escape hatch.
	Image      string         `gorm:"size:256" json:"image,omitempty"`
	ScriptPath string         `gorm:"size:256" json:"scriptPath,omitempty"`
	ScriptArgs datatypes.JSON `gorm:"not null;default:'[]'" json:"scriptArgs,omitempty"`
	Command    datatypes.JSON `gorm:"not null;default:'[]'" json:"command,omitempty"`
	Args       datatypes.JSON `gorm:"not null;default:'[]'" json:"args,omitempty"`
	Produces   datatypes.JSON `gorm:"not null;default:'[]'" json:"produces,omitempty"`
	Consumes   datatypes.JSON `gorm:"not null;default:'[]'" json:"consumes,omitempty"`

	// Release tasks only: chart/manifest source + values, serialized as the
	// runner's ReleaseSpec JSON.
	ReleaseConfig datatypes.JSON `json:"releaseConfig,omitempty"`

	// Canary (progressive rollout) for Release tasks; serialized as the
	// runner's RolloutSpec JSON.
	RolloutConfig datatypes.JSON `json:"rolloutConfig,omitempty"`

	// Approval tasks only.
	ApprovalConfig datatypes.JSON `json:"approvalConfig,omitempty"`

	RetryPolicy    datatypes.JSON `gorm:"not null;default:'{\"maxRetries\":0}'" json:"retryPolicy,omitempty"`
	TimeoutSeconds int            `gorm:"not null;default:0" json:"timeoutSeconds"`
}

func (PipelineTaskTemplate) TableName() string { return "pipeline_task_templates" }
