package models

import (
	"time"

	"github.com/google/uuid"

	runnerapi "github.com/rouroumaibing/software-distribution-platform-runner/api/v1alpha1"
)

// TaskRun is the hub-side persisted record of a runner TaskRun CR — one
// row per DAG node execution. Managed entirely through its parent
// PipelineRun's service, no standalone CRUD API.
type TaskRun struct {
	ID             uuid.UUID  `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"id"`
	PipelineRunID  uuid.UUID  `gorm:"type:uuid;not null;index" json:"pipelineRunId"`
	TaskTemplateID *uuid.UUID `gorm:"type:uuid" json:"taskTemplateId,omitempty"`
	CRName         string     `gorm:"size:256;not null" json:"crName"`

	TaskName string `gorm:"size:128;not null" json:"taskName"`
	// StageName is a DENORMALIZED SNAPSHOT of the stage this task ran in at
	// trigger time. It is NOT a foreign key to pipeline_stages and MUST NOT be
	// treated as one: a PipelineRun outlives the (versioned) definition it was
	// built from, so the authoritative stage is always recovered via
	// TaskTemplateID -> PipelineTaskTemplate.StageID. Keep this field for fast
	// per-stage queries / console grouping only.
	StageName string `gorm:"size:128;not null;index" json:"stageName"`

	Type  runnerapi.PipelineTaskType `gorm:"size:32;not null" json:"type"`
	Phase runnerapi.TaskRunPhase     `gorm:"size:32;not null;default:Pending" json:"phase"`

	RetryCount int    `gorm:"not null;default:0" json:"retryCount"`
	ExitCode   *int32 `json:"exitCode,omitempty"`

	StartTime      *time.Time `json:"startTime,omitempty"`
	CompletionTime *time.Time `json:"completionTime,omitempty"`
	LogsRef        string     `gorm:"size:512" json:"logsRef,omitempty"`
	Message        string     `json:"message,omitempty"`

	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

func (TaskRun) TableName() string { return "task_runs" }
