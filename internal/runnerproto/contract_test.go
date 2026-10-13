package runnerproto

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
)

// roundTrip decodes a runner-produced JSON sample into a hub-side runnerproto
// type and re-encodes it, asserting the wire form is byte-equivalent (modulo
// whitespace). This proves the hub copy is a faithful projection of runner's
// api/v1alpha1: any missing field, extra field, or divergent JSON tag breaks
// the equality check. DisallowUnknownFields makes the decode fail loudly if
// runner ever adds a field the hub copy lacks.
func roundTrip(t *testing.T, file string, v any) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", "contract", file))
	if err != nil {
		t.Fatalf("read %s: %v", file, err)
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		t.Fatalf("decode %s into %T: %v", file, v, err)
	}
	out, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("re-encode %s: %v", file, err)
	}
	assert.JSONEq(t, string(raw), string(out),
		"hub runnerproto type %T is NOT wire-compatible with runner's %s", v, file)
}

func TestContractApplyPipelineRunPayload(t *testing.T) {
	roundTrip(t, "apply_pipeline_run_payload.json", &ApplyPipelineRunPayload{})
}

func TestContractApproveTaskPayload(t *testing.T) {
	roundTrip(t, "approve_task_payload.json", &ApproveTaskPayload{})
}

func TestContractRolloutControlPayload(t *testing.T) {
	roundTrip(t, "rollout_control_payload.json", &RolloutControlPayload{})
}

func TestContractRerunTaskPayload(t *testing.T) {
	roundTrip(t, "rerun_task_payload.json", &RerunTaskPayload{})
}

func TestContractCancelPipelineRunPayload(t *testing.T) {
	roundTrip(t, "cancel_pipeline_run_payload.json", &CancelPipelineRunPayload{})
}

func TestContractAgentOpDispatchPayload(t *testing.T) {
	roundTrip(t, "agent_op_dispatch_payload.json", &AgentOpDispatchPayload{})
}

func TestContractStatusUpdatePayload(t *testing.T) {
	roundTrip(t, "status_update_payload.json", &StatusUpdatePayload{})
}

func TestContractLogChunkPayload(t *testing.T) {
	roundTrip(t, "log_chunk_payload.json", &LogChunkPayload{})
}

func TestContractAgentOpStatusPayload(t *testing.T) {
	roundTrip(t, "agent_op_status_payload.json", &AgentOpStatusPayload{})
}

func TestContractAgentOpLogPayload(t *testing.T) {
	roundTrip(t, "agent_op_log_payload.json", &AgentOpLogPayload{})
}

func TestContractAgentInfoPayload(t *testing.T) {
	roundTrip(t, "agent_info_payload.json", &AgentInfoPayload{})
}

func TestContractRolloutSpec(t *testing.T) {
	roundTrip(t, "rollout_spec.json", &RolloutSpec{})
}

func TestContractMessageApply(t *testing.T) {
	roundTrip(t, "message_apply.json", &Message{})
}

func TestContractMessageStatus(t *testing.T) {
	roundTrip(t, "message_status.json", &Message{})
}

// TestEnumValues locks the string values of every enum the hub persists or
// branches on. These strings may already be in DB rows / gateway frames, so a
// value change is a breaking wire change and must not happen silently.
func TestEnumValues(t *testing.T) {
	phase := map[PipelineRunPhase]string{
		PipelineRunPending: "Pending", PipelineRunRunning: "Running",
		PipelineRunWaitingApproval: "WaitingApproval", PipelineRunSucceeded: "Succeeded",
		PipelineRunFailed: "Failed", PipelineRunCancelled: "Cancelled",
	}
	for k, want := range phase {
		assert.Equal(t, want, string(k))
	}
	task := map[TaskRunPhase]string{
		TaskRunPending: "Pending", TaskRunRunning: "Running",
		TaskRunSucceeded: "Succeeded", TaskRunFailed: "Failed", TaskRunSkipped: "Skipped",
	}
	for k, want := range task {
		assert.Equal(t, want, string(k))
	}
	rollout := map[RolloutPhase]string{
		RolloutProgressing: "Progressing", RolloutPaused: "Paused", RolloutHealthy: "Healthy",
		RolloutDegraded: "Degraded", RolloutRollingBack: "RollingBack",
		RolloutFailed: "Failed", RolloutRolledBack: "RolledBack",
	}
	for k, want := range rollout {
		assert.Equal(t, want, string(k))
	}
	tt := map[PipelineTaskType]string{
		TaskTypeBuild: "Build", TaskTypeRelease: "Release",
		TaskTypeApproval: "Approval", TaskTypeTest: "Test",
	}
	for k, want := range tt {
		assert.Equal(t, want, string(k))
	}
	em := map[ExecutionMode]string{
		ExecutionModeParallel: "Parallel", ExecutionModeSerial: "Serial",
	}
	for k, want := range em {
		assert.Equal(t, want, string(k))
	}
	ra := map[RolloutAction]string{
		RolloutActionPause: "pause", RolloutActionPromote: "promote", RolloutActionRollback: "rollback",
	}
	for k, want := range ra {
		assert.Equal(t, want, string(k))
	}
}
