package runnerproto

// PipelineRunPhase represents the overall lifecycle phase of a PipelineRun.
type PipelineRunPhase string

const (
	PipelineRunPending         PipelineRunPhase = "Pending"
	PipelineRunRunning         PipelineRunPhase = "Running"
	PipelineRunWaitingApproval PipelineRunPhase = "WaitingApproval"
	PipelineRunSucceeded       PipelineRunPhase = "Succeeded"
	PipelineRunFailed          PipelineRunPhase = "Failed"
	PipelineRunCancelled       PipelineRunPhase = "Cancelled"
)

// TaskRunPhase represents the lifecycle phase of a single TaskRun.
type TaskRunPhase string

const (
	TaskRunPending   TaskRunPhase = "Pending"
	TaskRunRunning   TaskRunPhase = "Running"
	TaskRunSucceeded TaskRunPhase = "Succeeded"
	TaskRunFailed    TaskRunPhase = "Failed"
	// TaskRunSkipped is used when an upstream dependency failed, so this
	// task never becomes runnable.
	TaskRunSkipped TaskRunPhase = "Skipped"
)

// RolloutPhase represents the lifecycle phase of a Rollout.
type RolloutPhase string

const (
	RolloutProgressing RolloutPhase = "Progressing"
	RolloutPaused      RolloutPhase = "Paused"
	RolloutHealthy     RolloutPhase = "Healthy"
	RolloutDegraded    RolloutPhase = "Degraded"
	RolloutRollingBack RolloutPhase = "RollingBack"

	// Terminal states: Failed stops a rollout whose health checks are past
	// threshold without auto-rollback; RolledBack marks a completed revert to
	// 0% canary weight. Both are end states.
	RolloutFailed     RolloutPhase = "Failed"
	RolloutRolledBack RolloutPhase = "RolledBack"
)

// PipelineTaskType defines the category of a task within the DAG.
type PipelineTaskType string

const (
	TaskTypeBuild    PipelineTaskType = "Build"
	TaskTypeRelease  PipelineTaskType = "Release"
	TaskTypeApproval PipelineTaskType = "Approval"
	// TaskTypeTest runs a test/verification command the same way a Build task
	// runs its script — it's a pod-executed command whose exit code decides
	// pass/fail.
	TaskTypeTest PipelineTaskType = "Test"
)

// ExecutionMode controls how the tasks that share a Stage are scheduled
// relative to each other. Cross-stage ordering is always serial.
type ExecutionMode string

const (
	// ExecutionModeParallel (the default — also the zero value) lets every
	// task in a stage start as soon as its DependsOn is satisfied.
	ExecutionModeParallel ExecutionMode = "Parallel"
	// ExecutionModeSerial runs the stage's tasks one at a time, in the order
	// they appear in Spec.Tasks.
	ExecutionModeSerial ExecutionMode = "Serial"
)
