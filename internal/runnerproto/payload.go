package runnerproto

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

// StatusUpdatePayload is sent Runner -> Hub over MessageStatusUpdate. It
// carries the live phase of a PipelineRun plus a per-task progress summary,
// so the hub can keep its pipeline_runs / task_runs history rows in sync
// without the runner shipping raw CRD objects over the wire.
type StatusUpdatePayload struct {
	TargetID             string                 `json:"targetID"`
	PipelineRunName      string                 `json:"pipelineRunName"`
	PipelineRunNamespace string                 `json:"pipelineRunNamespace"`
	Phase                PipelineRunPhase       `json:"phase"`
	Message              string                 `json:"message,omitempty"`
	StartTime            *metav1.Time           `json:"startTime,omitempty"`
	CompletionTime       *metav1.Time           `json:"completionTime,omitempty"`
	Tasks                []TaskRunStatusSummary `json:"tasks,omitempty"`
}

// LogChunkPayload is sent Runner -> Hub over MessageLogChunk. Live logs are
// streamed per chunk rather than archived; the hub stores them for display.
type LogChunkPayload struct {
	TargetID        string `json:"targetID"`
	PipelineRunName string `json:"pipelineRunName"`
	TaskName        string `json:"taskName,omitempty"`
	Stream          string `json:"stream,omitempty"` // "stdout" | "stderr"
	Chunk           string `json:"chunk"`
}

// ApplyPipelineRunPayload is the Hub -> Runner envelope carried by
// MessageApplyPipelineRun. Name/Namespace identify the PipelineRun CR the
// Runner must create in its own cluster; they MUST match the Hub's
// pipeline_runs row (CRName / CRNamespace) so the status updates the Runner
// streams back route to the correct run on the Hub side. Spec is the fully
// resolved DAG to execute.
type ApplyPipelineRunPayload struct {
	Name      string          `json:"name"`
	Namespace string          `json:"namespace"`
	Spec      PipelineRunSpec `json:"spec"`
	// PublishVersion is the hub-side pipeline/publish version this run was
	// triggered from. It's an optional, immutable marker captured by the
	// ApplyHandler as a snapshot annotation so the exact stage/task
	// definitions that produced this run are reconstructable later.
	PublishVersion string `json:"publishVersion,omitempty"`
}

// ApproveTaskPayload is the Hub -> Runner envelope carried by
// MessageApproveTask. It records which human approved/rejected a manual
// approval task; the Runner patches the corresponding TaskRun.Approval status,
// which the PipelineRun controller observes to advance (or fail) the DAG.
type ApproveTaskPayload struct {
	PipelineRunName string `json:"pipelineRunName"`
	TaskName        string `json:"taskName"`
	// Approver is the hub-resolved identity (email/user id) of the person
	// who made this decision; recorded in TaskRun.Status.Approval.ApprovedBy.
	Approver string `json:"approver"`
	// Rejected, when true, fails the PipelineRun immediately.
	Rejected bool `json:"rejected,omitempty"`
}

// RolloutAction is the operator command carried by RolloutControlPayload.
type RolloutAction string

const (
	RolloutActionPause    RolloutAction = "pause"    // hold the rollout at the current weight
	RolloutActionPromote  RolloutAction = "promote"  // resume / advance past the current step
	RolloutActionRollback RolloutAction = "rollback" // revert to 0% canary weight and abort
)

// RolloutControlPayload is the Hub -> Runner envelope carried by
// MessageRolloutControl. It relays an operator's progressive-delivery
// command for a Release-type task's Rollout CR, identified by the owning
// PipelineRun CR name and the task name.
type RolloutControlPayload struct {
	PipelineRunName string        `json:"pipelineRunName"`
	TaskName        string        `json:"taskName"`
	Action          RolloutAction `json:"action"`
	// Operator is the hub-resolved identity of who issued the command;
	// recorded in the Rollout's status message for audit.
	Operator string `json:"operator,omitempty"`
}

// RerunTaskPayload is the Hub -> Runner envelope carried by MessageRerunTask.
// It asks the Runner to reset a single task (and, transitively, every
// downstream task that depends on it) so the DAG picks it up again — without
// re-dispatching the entire PipelineRun.
type RerunTaskPayload struct {
	PipelineRunName string `json:"pipelineRunName"`
	TaskName        string `json:"taskName"`
	// Operator is the hub-resolved identity issuing the rerun (audit only).
	Operator string `json:"operator,omitempty"`
}

// CancelPipelineRunPayload is the Hub -> Runner envelope carried by
// MessageCancelPipelineRun. It asks the Runner to stop the named PipelineRun.
type CancelPipelineRunPayload struct {
	PipelineRunName string `json:"pipelineRunName"`
	// Namespace is the cluster namespace holding the PipelineRun CR. The Hub
	// knows it from its pipeline_runs row (CRNamespace), so the Runner needs
	// no cluster-wide lookup to resolve the CR.
	Namespace string `json:"namespace"`
	// Operator is the hub-resolved identity issuing the cancel (audit only).
	Operator string `json:"operator,omitempty"`
}

// AgentOpDispatchPayload is the Hub -> Runner envelope carried by
// MessageAgentOp. OpID identifies the hub-side agent_ops ledger row the
// Runner's status/log reports must reference. Detail carries the op argument
// verbatim (the command or script for exec). Kubeconfig is set only for
// kubeconfig-access environments.
type AgentOpDispatchPayload struct {
	OpID       string `json:"opID"`
	TargetID   string `json:"targetID"`
	EnvID      string `json:"envID,omitempty"`
	OpType     string `json:"opType"` // exec (install/upgrade dispatch is deferred to the bootstrap flow)
	Detail     string `json:"detail"`
	Namespace  string `json:"namespace,omitempty"` // where the op's Job runs; hub supplies the environment namespace
	Kubeconfig []byte `json:"kubeconfig,omitempty"`
}

// AgentOpStatusPayload is the Runner -> Hub envelope carried by
// MessageAgentOpStatus: one lifecycle transition of a dispatched op.
type AgentOpStatusPayload struct {
	OpID    string `json:"opID"`
	Status  string `json:"status"` // running | succeeded | failed
	Message string `json:"message,omitempty"`
}

// AgentInfoPayload is the Runner -> Hub envelope carried by MessageAgentInfo:
// a one-shot self-identification frame sent immediately after every successful
// (re)connect. The hub uses it to persist targets.agent_version / last_seen_at
// and to reconcile a pending upgrade op.
type AgentInfoPayload struct {
	AgentVersion string   `json:"agentVersion"`
	OS           string   `json:"os"`
	Arch         string   `json:"arch"`
	Capabilities []string `json:"capabilities,omitempty"`
	TargetName   string   `json:"targetName,omitempty"`
	GoVersion    string   `json:"goVersion,omitempty"`
}

// AgentOpLogPayload is the Runner -> Hub envelope carried by MessageAgentOpLog:
// one streamed output chunk of a running op.
type AgentOpLogPayload struct {
	OpID   string `json:"opID"`
	Stream string `json:"stream,omitempty"` // stdout | stderr
	Chunk  string `json:"chunk"`
}
