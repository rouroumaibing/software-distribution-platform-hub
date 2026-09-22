package models

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"
)

// DispatchJobState enumerates the lifecycle of a durable dispatch record.
type DispatchJobState string

const (
	// DispatchJobPending means the job is queued and awaiting delivery.
	DispatchJobPending DispatchJobState = "pending"
	// DispatchJobDispatching means a delivery attempt is in flight (claimed).
	DispatchJobDispatching DispatchJobState = "dispatching"
	// DispatchJobDispatched means the payload was successfully sent to the Runner.
	DispatchJobDispatched DispatchJobState = "dispatched"
	// DispatchJobFailed means a delivery attempt failed and it is awaiting backoff retry.
	DispatchJobFailed DispatchJobState = "failed"
	// DispatchJobDead means retries are exhausted; the job is retired.
	DispatchJobDead DispatchJobState = "dead"
)

// DispatchJob is the durable record of one attempt to deliver a PipelineRun's
// spec to its target's Runner. Trigger enqueues a pending job instead
// of dispatching inline; a background sweeper (or an immediate redelivery when
// the Runner reconnects) delivers it. This decouples run creation from Runner
// connectivity: a run survives a temporarily-offline target and is delivered
// once the Runner comes back, instead of failing the whole trigger.
type DispatchJob struct {
	ID uuid.UUID `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"id"`

	PipelineRunID uuid.UUID `gorm:"type:uuid;not null;index" json:"pipelineRunId"`
	// TargetID is the target environment id (== the Runner's registered target id).
	TargetID uuid.UUID `gorm:"type:uuid;not null;index" json:"targetId"`

	// Payload is the exact ApplyPipelineRunPayload the Runner must receive,
	// stored so redelivery needs no re-derivation of the spec.
	Payload datatypes.JSON `gorm:"type:jsonb;not null" json:"-"`

	State DispatchJobState `gorm:"size:32;not null;default:pending;index" json:"state"`

	Attempts    int        `gorm:"not null;default:0" json:"attempts"`
	LastError   string     `gorm:"size:512" json:"lastError,omitempty"`
	NextRetryAt *time.Time `json:"nextRetryAt,omitempty"`

	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

func (DispatchJob) TableName() string { return "dispatch_jobs" }
