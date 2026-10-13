package runnerproto

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

// TaskRunStatusSummary is an embedded, lightweight view of a TaskRun's
// progress, kept in sync by the PipelineRun controller so the whole DAG
// state is visible from a single view without needing to list TaskRuns
// separately.
type TaskRunStatusSummary struct {
	Name           string       `json:"name"`
	TaskRunRef     string       `json:"taskRunRef,omitempty"`
	Phase          TaskRunPhase `json:"phase"`
	RetryCount     int32        `json:"retryCount,omitempty"`
	StartTime      *metav1.Time `json:"startTime,omitempty"`
	CompletionTime *metav1.Time `json:"completionTime,omitempty"`
	Message        string       `json:"message,omitempty"`

	// Rollout is the live progressive-delivery snapshot for Deploy-type
	// tasks; optional — absent for non-deploy tasks and for old runners
	// (omitempty keeps the wire format backward compatible).
	Rollout *RolloutStatusSummary `json:"rollout,omitempty"`
}

// RolloutStatusSummary is the reflux projection of a Rollout CR's status at
// report time. Snapshot semantics: the hub upserts it onto the task's
// rollout_runs row (last-write-wins); the hub accumulates the step history
// server-side so the runner stays stateless.
type RolloutStatusSummary struct {
	Phase            RolloutPhase `json:"phase"`
	CurrentStepIndex int          `json:"currentStepIndex"`
	CurrentWeight    int          `json:"currentWeight"`
	WorkloadRef      string       `json:"workloadRef,omitempty"`
	Message          string       `json:"message,omitempty"`
}
