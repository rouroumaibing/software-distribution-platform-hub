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
// It embeds common.Base (soft delete) rather than BaseNoSoftDelete: once
// PipelineStage moved to soft delete (DELETE-CONTRACT §6.6-3 决策 3), a
// hard-deleted template under a soft-deleted stage would be the same
// "half state" the contract rejects. The DDL's
// `stage_id ... on delete cascade` never fires for soft deletes, so
// StageService soft-deletes a stage's templates explicitly.
//
// Before this, the struct also carried no created_at / updated_at mapping;
// BaseNoSoftDelete was added on 2026-09-16 to expose them, and Base keeps
// both plus deleted_at.
type PipelineTaskTemplate struct {
	common.Base
	// 与 pipeline_stages / pipelines 同手法：唯一性只作用于未软删的行，否则
	// 软删一个模板后同 stage 下再建同名模板会撞 0001 里的
	// `unique (stage_id, name)` → 500。索引由 StageID + Name **两个字段共同
	// 声明**（列序 = struct 字段序），DDL 侧同义声明见 migrations/0009。
	StageID uuid.UUID `gorm:"type:uuid;not null;index;uniqueIndex:idx_task_templates_stage_name_active,where:deleted_at IS NULL" json:"stageId"`

	Name         string                     `gorm:"size:128;not null;uniqueIndex:idx_task_templates_stage_name_active,where:deleted_at IS NULL" json:"name"`
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

	// Privileged (G-4 最小版) runs the task container with
	// securityContext.privileged=true, enabling docker-in-docker /
	// containerd-in-containerd build images. Off by default; opt-in per task.
	Privileged bool `gorm:"not null;default:false" json:"privileged,omitempty"`
}

func (PipelineTaskTemplate) TableName() string { return "pipeline_task_templates" }
