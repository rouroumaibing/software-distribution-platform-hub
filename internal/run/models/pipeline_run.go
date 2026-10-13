package models

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"

	runnerapi "github.com/rouroumaibing/software-distribution-platform-hub/internal/runnerproto"
)

// PipelineRun is the hub-side persisted record of a runner PipelineRun CR;
// powers the "运行记录" list. The CR itself is only kept in-cluster for a
// short TTL — this table is the long-term source of truth.
type PipelineRun struct {
	ID         uuid.UUID `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"id"`
	PipelineID uuid.UUID `gorm:"type:uuid;not null;index" json:"pipelineId"`
	TargetID   uuid.UUID `gorm:"type:uuid;not null" json:"targetId"`

	// 反规范化归属（ui-12 §5.1 环境覆盖档 / §6.3 配额 org 维度）：触发时按
	// (component, target, namespace) 三元组解析环境、沿 pipeline→component
	// 解析 org；解析不到/歧义留空（fail-open——归属是统计与指派精度问题，
	// 不挡触发）。见 run service 的 EnvironmentMetaStore。
	OrgID         *uuid.UUID `gorm:"type:uuid;index" json:"orgId,omitempty"`
	EnvironmentID *uuid.UUID `gorm:"type:uuid;index" json:"environmentId,omitempty"`

	// Bridges this row to the live K8s object: kubectl get pipelinerun
	// {CRName} -n {CRNamespace} --context {target}.
	CRName      string `gorm:"size:256;not null" json:"crName"`
	CRNamespace string `gorm:"size:128;not null" json:"crNamespace"`

	CommitSHA string         `gorm:"size:64" json:"commitSha,omitempty"`
	Params    datatypes.JSON `gorm:"not null;default:'{}'" json:"params,omitempty"`

	// Records which pipelines.version this run's DAG was built from.
	PipelineVersion int `json:"pipelineVersion"`

	Phase       runnerapi.PipelineRunPhase `gorm:"size:32;not null;default:Pending;index" json:"phase"`
	TriggeredBy string                     `gorm:"size:128" json:"triggeredBy,omitempty"`
	// TriggerSource is the invocation channel (manual/cron/webhook/event) —
	// the structured half of the 运行列表「触发方式」column (ui-12 §4.3).
	TriggerSource string `gorm:"size:16;not null;default:manual" json:"triggerSource,omitempty"`

	StartTime      *time.Time `json:"startTime,omitempty"`
	CompletionTime *time.Time `json:"completionTime,omitempty"`
	Message        string     `json:"message,omitempty"`

	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

func (PipelineRun) TableName() string { return "pipeline_runs" }
