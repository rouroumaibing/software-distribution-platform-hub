package models

import (
	"time"

	"github.com/google/uuid"

	runnerapi "github.com/rouroumaibing/software-distribution-platform-runner/api/v1alpha1"
)

// RolloutRun records progressive-delivery history for a Deploy-type
// TaskRun, so the console can show how canary weight moved over time.
type RolloutRun struct {
	ID          uuid.UUID `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"id"`
	TaskRunID   uuid.UUID `gorm:"type:uuid;not null;index" json:"taskRunId"`
	WorkloadRef string    `gorm:"size:256;not null" json:"workloadRef"`

	Phase            runnerapi.RolloutPhase `gorm:"size:32;not null;default:Progressing" json:"phase"`
	CurrentStepIndex int                    `gorm:"not null;default:0" json:"currentStepIndex"`
	CurrentWeight    int                    `gorm:"not null;default:0" json:"currentWeight"`

	StartTime      *time.Time `json:"startTime,omitempty"`
	CompletionTime *time.Time `json:"completionTime,omitempty"`
}

func (RolloutRun) TableName() string { return "rollout_runs" }
