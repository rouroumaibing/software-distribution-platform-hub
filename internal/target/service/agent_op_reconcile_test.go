package service

// install/upgrade 对账单测（INSTALL-UPGRADE-EXECUTOR-DESIGN §2.2 / §4.2）：
//   - upgrade 对账 = agent_info 上报版本匹配 Detail 的 running op → succeeded；
//   - install 对账 = runner 首连 → queued install op → succeeded；
//   - 超时对账 = 超过 maxAge 未重连的 running upgrade op → failed。

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/rouroumaibing/software-distribution-platform-hub/internal/target/models"
)

func TestReconcileUpgrade_SucceedsMatchingVersion(t *testing.T) {
	repo := newFakeAgentOpRepo()
	svc := NewAgentOpService(repo)
	target := uuid.New()
	op := &models.AgentOp{ID: uuid.New(), TargetID: target, OpType: models.AgentOpUpgrade, Status: models.AgentOpRunning, Detail: "v0.0.2"}
	repo.ops[op.ID] = op

	if err := svc.ReconcileUpgrade(target, "v0.0.2"); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if op.Status != models.AgentOpSucceeded {
		t.Fatalf("op status = %q, want succeeded", op.Status)
	}
}

func TestReconcileUpgrade_IgnoresVersionMismatch(t *testing.T) {
	repo := newFakeAgentOpRepo()
	svc := NewAgentOpService(repo)
	target := uuid.New()
	op := &models.AgentOp{ID: uuid.New(), TargetID: target, OpType: models.AgentOpUpgrade, Status: models.AgentOpRunning, Detail: "v0.0.2"}
	repo.ops[op.ID] = op

	if err := svc.ReconcileUpgrade(target, "v0.0.9"); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if op.Status != models.AgentOpRunning {
		t.Fatalf("version mismatch must keep op running, got %q", op.Status)
	}
}

func TestReconcileInstall_SucceedsQueuedInstall(t *testing.T) {
	repo := newFakeAgentOpRepo()
	svc := NewAgentOpService(repo)
	target := uuid.New()
	op := &models.AgentOp{ID: uuid.New(), TargetID: target, OpType: models.AgentOpInstall, Status: models.AgentOpQueued, Detail: "v0.0.1"}
	repo.ops[op.ID] = op

	if err := svc.ReconcileInstall(target); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if op.Status != models.AgentOpSucceeded {
		t.Fatalf("install op status = %q, want succeeded", op.Status)
	}
}

func TestSweepUpgradeTimeouts_FailsStaleRunning(t *testing.T) {
	repo := newFakeAgentOpRepo()
	svc := NewAgentOpService(repo)
	target := uuid.New()
	stale := &models.AgentOp{ID: uuid.New(), TargetID: target, OpType: models.AgentOpUpgrade, Status: models.AgentOpRunning, Detail: "v0.0.2"}
	stale.UpdatedAt = time.Now().Add(-30 * time.Minute)
	repo.ops[stale.ID] = stale

	if err := svc.SweepUpgradeTimeouts(context.Background(), 15*time.Minute); err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if stale.Status != models.AgentOpFailed {
		t.Fatalf("stale upgrade must fail, got %q", stale.Status)
	}
}

func TestSweepUpgradeTimeouts_KeepsFreshRunning(t *testing.T) {
	repo := newFakeAgentOpRepo()
	svc := NewAgentOpService(repo)
	target := uuid.New()
	fresh := &models.AgentOp{ID: uuid.New(), TargetID: target, OpType: models.AgentOpUpgrade, Status: models.AgentOpRunning, Detail: "v0.0.2"}
	fresh.UpdatedAt = time.Now().Add(-2 * time.Minute)
	repo.ops[fresh.ID] = fresh

	if err := svc.SweepUpgradeTimeouts(context.Background(), 15*time.Minute); err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if fresh.Status != models.AgentOpRunning {
		t.Fatalf("fresh upgrade must stay running, got %q", fresh.Status)
	}
}
