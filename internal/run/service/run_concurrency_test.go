package service

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/rouroumaibing/software-distribution-platform-hub/internal/common"
	pipelinemodels "github.com/rouroumaibing/software-distribution-platform-hub/internal/pipeline/models"
	"github.com/rouroumaibing/software-distribution-platform-hub/internal/run/models"
	runnerapi "github.com/rouroumaibing/software-distribution-platform-hub/internal/runnerproto"
)

// policyPipelineDefStore returns a pipeline with a fixed O-41 concurrency
// policy (fakePipelineDefStore always yields an empty policy = allow).
type policyPipelineDefStore struct{ policy string }

func (f policyPipelineDefStore) GetByID(uuid.UUID) (*pipelinemodels.Pipeline, error) {
	return &pipelinemodels.Pipeline{
		Name:              "p",
		Version:           1,
		ComponentID:       uuid.New(),
		ConcurrencyPolicy: f.policy,
	}, nil
}

// TestTriggerForbidRejectsSecondRun: with policy=forbid, a trigger while an
// active run exists is rejected with 409 before anything is persisted.
func TestTriggerForbidRejectsSecondRun(t *testing.T) {
	store := newFakeDispatchStore()
	runStore := newFakePipelineRunStore()
	svc := newFullTestService(&fakeDispatcher{fail: true}, store)
	svc.repo = runStore
	svc.pipelineRepo = policyPipelineDefStore{policy: pipelinemodels.ConcurrencyForbid}

	pid := uuid.New()
	req := &models.TriggerRequest{TargetIDs: []uuid.UUID{uuid.New()}, TriggeredBy: "tester"}
	if _, err := svc.Trigger(pid, req); err != nil {
		t.Fatalf("first trigger under forbid must succeed: %v", err)
	}
	_, err := svc.Trigger(pid, req)
	var ae *common.APIError
	if !errors.As(err, &ae) || ae.Code != http.StatusConflict {
		t.Fatalf("second trigger under forbid must be 409, got %v", err)
	}
	if len(runStore.runs) != 1 {
		t.Fatalf("forbid must not persist the rejected run, got %d runs", len(runStore.runs))
	}
}

// TestTriggerQueueParksAndPromotes: with policy=queue and limit N=1, the
// second trigger parks as PendingQueue with a held dispatch job; once the
// first run reaches a terminal phase, SweepQueued promotes the queued run
// back to Pending and releases the job for delivery.
func TestTriggerQueueParksAndPromotes(t *testing.T) {
	store := newFakeDispatchStore()
	runStore := newFakePipelineRunStore()
	svc := newFullTestService(&fakeDispatcher{fail: true}, store)
	svc.repo = runStore
	svc.pipelineRepo = policyPipelineDefStore{policy: pipelinemodels.ConcurrencyQueue}
	svc.SetRunConcurrencyLimit(1)

	pid := uuid.New()
	req := &models.TriggerRequest{TargetIDs: []uuid.UUID{uuid.New()}, TriggeredBy: "tester"}
	first, err := svc.Trigger(pid, req)
	if err != nil {
		t.Fatalf("first trigger: %v", err)
	}
	if first[0].Phase != runnerapi.PipelineRunPending {
		t.Fatalf("first run must be Pending, got %s", first[0].Phase)
	}
	second, err := svc.Trigger(pid, req)
	if err != nil {
		t.Fatalf("second trigger under queue must succeed (park): %v", err)
	}
	if second[0].Phase != runnerapi.PipelineRunPendingQueue {
		t.Fatalf("second run must park as PendingQueue, got %s", second[0].Phase)
	}
	var held *models.DispatchJob
	for _, j := range store.all() {
		if j.PipelineRunID == second[0].ID {
			cp := j
			held = &cp
		}
	}
	if held == nil || held.State != models.DispatchJobHeld {
		t.Fatalf("queued run's dispatch job must be held, got %+v", held)
	}

	// First run finishes → capacity frees → sweep promotes the queued run.
	done := *first[0]
	done.Phase = runnerapi.PipelineRunFailed
	if err := runStore.Update(&done); err != nil {
		t.Fatalf("mark first run failed: %v", err)
	}
	if err := svc.SweepQueued(context.Background()); err != nil {
		t.Fatalf("SweepQueued: %v", err)
	}
	got, err := runStore.GetByID(second[0].ID)
	if err != nil {
		t.Fatalf("reload queued run: %v", err)
	}
	if got.Phase != runnerapi.PipelineRunPending {
		t.Fatalf("queued run must be promoted to Pending, got %s", got.Phase)
	}
	released, _ := store.ReleaseHeld(got.ID) // zero rows = already released by the sweep
	if released != 0 {
		t.Fatalf("sweep must release the held job itself, still held after sweep")
	}
}

// TestSweepRunTimeoutsFailsAgedRun: only non-terminal runs whose
// COALESCE(start_time, created_at)+timeout has passed are failed with
// reason PipelineRunTimeout; fresh and timeout-less runs are untouched.
func TestSweepRunTimeoutsFailsAgedRun(t *testing.T) {
	runStore := newFakePipelineRunStore()
	svc := &PipelineRunService{repo: runStore}

	now := time.Now()
	aged := &models.PipelineRun{Phase: runnerapi.PipelineRunRunning, TimeoutSeconds: 60, StartTime: timePtr(now.Add(-2 * time.Hour))}
	fresh := &models.PipelineRun{Phase: runnerapi.PipelineRunRunning, TimeoutSeconds: 3600, StartTime: timePtr(now)}
	none := &models.PipelineRun{Phase: runnerapi.PipelineRunRunning}
	for _, r := range []*models.PipelineRun{aged, fresh, none} {
		if err := runStore.Create(r); err != nil {
			t.Fatalf("seed run: %v", err)
		}
	}
	n, err := svc.SweepRunTimeouts(context.Background(), now)
	if err != nil {
		t.Fatalf("SweepRunTimeouts: %v", err)
	}
	if n != 1 {
		t.Fatalf("exactly one aged run must be failed, got %d", n)
	}
	got, _ := runStore.GetByID(aged.ID)
	if got.Phase != runnerapi.PipelineRunFailed || got.Message != "PipelineRunTimeout" {
		t.Fatalf("aged run must fail with PipelineRunTimeout, got %s/%s", got.Phase, got.Message)
	}
	if got.CompletionTime == nil {
		t.Fatalf("timed-out run must get a completion time")
	}
	for _, id := range []uuid.UUID{fresh.ID, none.ID} {
		r, _ := runStore.GetByID(id)
		if r.Phase != runnerapi.PipelineRunRunning {
			t.Fatalf("run %s must stay Running, got %s", id, r.Phase)
		}
	}
}
