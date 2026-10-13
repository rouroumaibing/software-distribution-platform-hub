package runnerproto

import "encoding/json"

// MessageType distinguishes the small set of message shapes exchanged over
// the Hub-Spoke long connection. It lives in the shared api/v1alpha1
// package (not in either binary's internal connector) so the Hub and the
// Runner agree on exactly one wire format.
type MessageType string

const (
	// Hub -> Runner
	MessageApplyPipelineRun MessageType = "apply_pipeline_run"
	MessageApproveTask      MessageType = "approve_task"
	// MessageRerunTask asks the Runner to re-run a single failed/pending task
	// (and its downstream dependents) without re-dispatching the whole run.
	MessageRerunTask MessageType = "rerun_task"
	// MessageCancelPipelineRun asks the Runner to stop a running PipelineRun.
	MessageCancelPipelineRun MessageType = "cancel_pipeline_run"
	// MessageRolloutControl relays an operator's pause/promote/rollback
	// command for a Release task's Rollout CR.
	MessageRolloutControl MessageType = "rollout_control"
	// MessageAgentOp dispatches one agent operation to the Runner.
	MessageAgentOp MessageType = "agent_op"

	// Runner -> Hub
	MessageStatusUpdate MessageType = "status_update"
	MessageLogChunk     MessageType = "log_chunk"
	MessageHeartbeat    MessageType = "heartbeat"
	// MessageAgentOpStatus reports a lifecycle transition of a dispatched op.
	MessageAgentOpStatus MessageType = "agent_op_status"
	// MessageAgentOpLog streams one output chunk produced by an agent op.
	MessageAgentOpLog MessageType = "agent_op_log"
	// MessageAgentInfo is the one-shot identity frame the Runner sends on
	// every (re)connect.
	MessageAgentInfo MessageType = "agent_info"
)

// Message is the envelope for every frame on the connection: a typed
// discriminant plus an opaque JSON payload decoded by the receiving side
// according to Type.
type Message struct {
	Type    MessageType     `json:"type"`
	Payload json.RawMessage `json:"payload"`
}
