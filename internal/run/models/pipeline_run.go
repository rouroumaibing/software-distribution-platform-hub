package models

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"

	runnerapi "github.com/rouroumaibing/software-distribution-platform-runner/api/v1alpha1"
)

// PipelineRun is the hub-side persisted record of a runner PipelineRun CR;
// powers the "运行记录" list. The CR itself is only kept in-cluster for a
// short TTL — this table is the long-term source of truth.
type PipelineRun struct {
	ID         uuid.UUID `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"id"`
	PipelineID uuid.UUID `gorm:"type:uuid;not null;index" json:"pipelineId"`
	ClusterID  uuid.UUID `gorm:"type:uuid;not null" json:"clusterId"`

	// Bridges this row to the live K8s object: kubectl get pipelinerun
	// {CRName} -n {CRNamespace} --context {cluster}.
	CRName      string `gorm:"size:256;not null" json:"crName"`
	CRNamespace string `gorm:"size:128;not null" json:"crNamespace"`

	CommitSHA string         `gorm:"size:64" json:"commitSha,omitempty"`
	Params    datatypes.JSON `gorm:"not null;default:'{}'" json:"params,omitempty"`

	// Records which pipelines.version this run's DAG was built from.
	PipelineVersion int `json:"pipelineVersion"`

	Phase       runnerapi.PipelineRunPhase `gorm:"size:32;not null;default:Pending;index" json:"phase"`
	TriggeredBy string                     `gorm:"size:128" json:"triggeredBy,omitempty"`

	StartTime      *time.Time `json:"startTime,omitempty"`
	CompletionTime *time.Time `json:"completionTime,omitempty"`
	Message        string     `json:"message,omitempty"`

	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

func (PipelineRun) TableName() string { return "pipeline_runs" }
