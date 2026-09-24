package service

// Exec（§9.5 直连执行）的服务层单测：校验请求形状、落 agent_ops 台账、
// 未注入 store 时显式报错（而不是静默丢请求）。

import (
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/rouroumaibing/software-distribution-platform-hub/internal/common"
	targetmodels "github.com/rouroumaibing/software-distribution-platform-hub/internal/target/models"
)

type fakeAgentOps struct {
	created []*targetmodels.AgentOp
	err     error
}

func (f *fakeAgentOps) Create(op *targetmodels.AgentOp) error {
	if f.err != nil {
		return f.err
	}
	f.created = append(f.created, op)
	return nil
}

func TestExec_RequiresCommandOrScript(t *testing.T) {
	env := agentEnv()
	svc := NewEnvironmentService(newFakeEnvStore(env), &fakeTargetLookup{}, nil)
	svc.SetAgentOpStore(&fakeAgentOps{})

	_, err := svc.Exec(env.ID, ExecRequest{})
	if err == nil {
		t.Fatal("empty body must be rejected")
	}
	if !strings.Contains(err.Error(), "command") && !strings.Contains(err.Error(), "script") {
		t.Fatalf("error should mention command/script, got %v", err)
	}
}

func TestExec_RecordsQueuedOpOnEnvTarget(t *testing.T) {
	env := agentEnv()
	ops := &fakeAgentOps{}
	svc := NewEnvironmentService(newFakeEnvStore(env), &fakeTargetLookup{}, nil)
	svc.SetAgentOpStore(ops)

	op, err := svc.Exec(env.ID, ExecRequest{Command: "kubectl get pods -n x"})
	if err != nil {
		t.Fatalf("exec: %v", err)
	}
	if len(ops.created) != 1 {
		t.Fatalf("expected 1 recorded op, got %d", len(ops.created))
	}
	got := ops.created[0]
	if got.TargetID != env.TargetID {
		t.Fatalf("op must anchor to the env's target, got %s want %s", got.TargetID, env.TargetID)
	}
	if got.EnvID == nil || *got.EnvID != env.ID {
		t.Fatalf("op must reference the env")
	}
	if got.OpType != targetmodels.AgentOpExec {
		t.Fatalf("op type must be exec, got %q", got.OpType)
	}
	if got.Status != targetmodels.AgentOpQueued {
		t.Fatalf("op must start queued, got %q", got.Status)
	}
	if got.Detail != "kubectl get pods -n x" {
		t.Fatalf("detail must carry the command verbatim, got %q", got.Detail)
	}
	if op == nil || op.TargetID != env.TargetID {
		t.Fatalf("returned handle must be the recorded op")
	}
}

func TestExec_ScriptFallsBackToDetail(t *testing.T) {
	env := agentEnv()
	ops := &fakeAgentOps{}
	svc := NewEnvironmentService(newFakeEnvStore(env), &fakeTargetLookup{}, nil)
	svc.SetAgentOpStore(ops)

	if _, err := svc.Exec(env.ID, ExecRequest{Script: "echo hi"}); err != nil {
		t.Fatalf("exec: %v", err)
	}
	if ops.created[0].Detail != "echo hi" {
		t.Fatalf("script must land in detail, got %q", ops.created[0].Detail)
	}
}

func TestExec_NotWiredIsExplicitError(t *testing.T) {
	env := agentEnv()
	svc := NewEnvironmentService(newFakeEnvStore(env), &fakeTargetLookup{}, nil)

	_, err := svc.Exec(env.ID, ExecRequest{Command: "ls"})
	if err == nil {
		t.Fatal("unwired store must error, not silently drop")
	}
	if common.ErrInternal != nil && !strings.Contains(err.Error(), "agent op store") {
		t.Fatalf("error should point at wiring, got %v", err)
	}
}

func TestExec_UnknownEnvIs404Class(t *testing.T) {
	svc := NewEnvironmentService(newFakeEnvStore(), &fakeTargetLookup{}, nil)
	svc.SetAgentOpStore(&fakeAgentOps{})

	if _, err := svc.Exec(uuid.New(), ExecRequest{Command: "ls"}); err == nil {
		t.Fatal("unknown env must error")
	}
}
