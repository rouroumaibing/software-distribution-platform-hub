package service

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"
	"gorm.io/gorm"

	runnerapi "github.com/rouroumaibing/software-distribution-platform-runner/api/v1alpha1"

	"github.com/rouroumaibing/software-distribution-platform-hub/internal/run/models"
)

var errOffline = errors.New("runner offline")

// fakeDispatcher records Dispatch calls and can be toggled to fail.
type fakeDispatcher struct {
	mu          sync.Mutex
	fail        bool
	dispatched  []uuid.UUID
	lastPayload *runnerapi.ApplyPipelineRunPayload
}

func (f *fakeDispatcher) Dispatch(ctx context.Context, targetID uuid.UUID, payload *runnerapi.ApplyPipelineRunPayload) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail {
		return errOffline
	}
	f.dispatched = append(f.dispatched, targetID)
	f.lastPayload = payload
	return nil
}

func (f *fakeDispatcher) Approve(ctx context.Context, targetID uuid.UUID, payload *runnerapi.ApproveTaskPayload) error {
	return nil
}

func (f *fakeDispatcher) RolloutControl(ctx context.Context, targetID uuid.UUID, payload *runnerapi.RolloutControlPayload) error {
	return nil
}

func (f *fakeDispatcher) RerunTask(ctx context.Context, targetID uuid.UUID, payload *runnerapi.RerunTaskPayload) error {
	return nil
}

func (f *fakeDispatcher) CancelPipelineRun(ctx context.Context, targetID uuid.UUID, payload *runnerapi.CancelPipelineRunPayload) error {
	return nil
}

func (f *fakeDispatcher) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.dispatched)
}

// fakeDispatchStore is an in-memory DispatchJobStore for offline unit tests.
type fakeDispatchStore struct {
	mu    sync.Mutex
	jobs  map[uuid.UUID]*models.DispatchJob
	order []uuid.UUID
}

func newFakeDispatchStore() *fakeDispatchStore {
	return &fakeDispatchStore{jobs: map[uuid.UUID]*models.DispatchJob{}}
}

func (s *fakeDispatchStore) Create(j *models.DispatchJob) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if j.ID == uuid.Nil {
		j.ID = uuid.New()
	}
	cp := *j
	s.jobs[cp.ID] = &cp
	s.order = append(s.order, cp.ID)
	return nil
}

func (s *fakeDispatchStore) GetByID(id uuid.UUID) (*models.DispatchJob, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	j, ok := s.jobs[id]
	if !ok {
		return nil, gorm.ErrRecordNotFound
	}
	cp := *j
	return &cp, nil
}

func (s *fakeDispatchStore) ListPendingByTarget(targetID uuid.UUID) ([]models.DispatchJob, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []models.DispatchJob
	for _, id := range s.order {
		j := s.jobs[id]
		if j.TargetID == targetID && (j.State == models.DispatchJobPending || j.State == models.DispatchJobFailed) {
			out = append(out, *j)
		}
	}
	return out, nil
}

func (s *fakeDispatchStore) MarkDispatching(id uuid.UUID) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	j, ok := s.jobs[id]
	if !ok {
		return 0, gorm.ErrRecordNotFound
	}
	if j.State != models.DispatchJobPending && j.State != models.DispatchJobFailed {
		return 0, nil
	}
	j.State = models.DispatchJobDispatching
	j.Attempts++
	return 1, nil
}

func (s *fakeDispatchStore) MarkDispatched(id uuid.UUID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	j := s.jobs[id]
	j.State = models.DispatchJobDispatched
	j.LastError = ""
	j.NextRetryAt = nil
	return nil
}

func (s *fakeDispatchStore) MarkFailed(id uuid.UUID, lastErr string, next time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	j := s.jobs[id]
	j.State = models.DispatchJobFailed
	j.LastError = lastErr
	j.NextRetryAt = &next
	return nil
}

func (s *fakeDispatchStore) MarkDead(id uuid.UUID, lastErr string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	j := s.jobs[id]
	j.State = models.DispatchJobDead
	j.LastError = lastErr
	j.NextRetryAt = nil
	return nil
}

func (s *fakeDispatchStore) ListDueRetry(now time.Time) ([]models.DispatchJob, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []models.DispatchJob
	for _, id := range s.order {
		j := s.jobs[id]
		if j.State == models.DispatchJobFailed && j.NextRetryAt != nil && !j.NextRetryAt.After(now) {
			out = append(out, *j)
		}
	}
	return out, nil
}

// LatestByRun returns the last job inserted for a run (insertion order == created
// order in the fake). Satisfies DispatchJobStore for Redispatch tests.
func (s *fakeDispatchStore) LatestByRun(pipelineRunID uuid.UUID) (*models.DispatchJob, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var found *models.DispatchJob
	for _, id := range s.order {
		j := s.jobs[id]
		if j.PipelineRunID == pipelineRunID {
			cp := *j
			found = &cp
		}
	}
	if found == nil {
		return nil, gorm.ErrRecordNotFound
	}
	return found, nil
}

func (s *fakeDispatchStore) all() []models.DispatchJob {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]models.DispatchJob, 0, len(s.order))
	for _, id := range s.order {
		out = append(out, *s.jobs[id])
	}
	return out
}

func (s *fakeDispatchStore) mustGet(t *testing.T, id uuid.UUID) *models.DispatchJob {
	t.Helper()
	j, err := s.GetByID(id)
	if err != nil {
		t.Fatalf("GetByID(%s): %v", id, err)
	}
	return j
}

func newTestService(disp Dispatcher, store DispatchJobStore) *PipelineRunService {
	return &PipelineRunService{
		dispatcher:    disp,
		dispatchRepo:  store,
		sweepInterval: 20 * time.Millisecond,
	}
}

func samplePayload() *runnerapi.ApplyPipelineRunPayload {
	return &runnerapi.ApplyPipelineRunPayload{
		Name:      "pr-x",
		Namespace: "sdp-run",
		Spec:      runnerapi.PipelineRunSpec{PipelineRef: "p"},
	}
}

func mustMarshal(t *testing.T, v any) datatypes.JSON {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return datatypes.JSON(b)
}

func timePtr(t time.Time) *time.Time { return &t }

// TestTriggerOfflineRunNotFailed: when the Runner is offline, enqueueDispatch
// must NOT fail the trigger — the run stays Pending and a recoverable (failed)
// job is recorded for later redelivery.
func TestTriggerOfflineRunNotFailed(t *testing.T) {
	store := newFakeDispatchStore()
	disp := &fakeDispatcher{fail: true}
	svc := newTestService(disp, store)

	runID, targetID := uuid.New(), uuid.New()
	run := &models.PipelineRun{ID: runID, Phase: runnerapi.PipelineRunPending}

	// Mirror of Trigger's tail: only a persistence error is fatal.
	err := svc.enqueueDispatch(run.ID, targetID, samplePayload())
	if err != nil {
		run.Phase = runnerapi.PipelineRunFailed
	}

	if err != nil {
		t.Fatalf("offline enqueue must not error: %v", err)
	}
	if run.Phase != runnerapi.PipelineRunPending {
		t.Fatalf("run must stay Pending when Runner offline, got %s", run.Phase)
	}
	jobs := store.all()
	if len(jobs) != 1 {
		t.Fatalf("expected 1 dispatch job, got %d", len(jobs))
	}
	if jobs[0].State == models.DispatchJobDispatched {
		t.Fatalf("offline job must not be dispatched, got %s", jobs[0].State)
	}
	if disp.count() != 0 {
		t.Fatalf("dispatcher must not be called when offline, got %d calls", disp.count())
	}
}

// TestDrainTargetRedeliversOnReconnect: a job enqueued while offline is
// delivered when the Runner reconnects (DrainTarget).
func TestDrainTargetRedeliversOnReconnect(t *testing.T) {
	store := newFakeDispatchStore()
	disp := &fakeDispatcher{fail: true} // start offline
	svc := newTestService(disp, store)

	runID, targetID := uuid.New(), uuid.New()
	if err := svc.enqueueDispatch(runID, targetID, samplePayload()); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	if disp.count() != 0 {
		t.Fatalf("should be offline, got %d dispatches", disp.count())
	}

	// Runner comes online.
	disp.fail = false
	if err := svc.DrainTarget(context.Background(), targetID); err != nil {
		t.Fatalf("drain: %v", err)
	}

	jobs := store.all()
	if len(jobs) != 1 {
		t.Fatalf("expected 1 job, got %d", len(jobs))
	}
	if jobs[0].State != models.DispatchJobDispatched {
		t.Fatalf("job should be dispatched after reconnect, got %s", jobs[0].State)
	}
	if disp.count() != 1 || disp.dispatched[0] != targetID {
		t.Fatalf("dispatcher should be called once for the target, got %v", disp.dispatched)
	}
}

// TestWriteFailureRetriesThenDead: repeated delivery failures exhaust retries
// and retire the job as dead; further delivery is a no-op.
func TestWriteFailureRetriesThenDead(t *testing.T) {
	store := newFakeDispatchStore()
	disp := &fakeDispatcher{fail: true}
	svc := newTestService(disp, store)

	runID, targetID := uuid.New(), uuid.New()
	if err := store.Create(&models.DispatchJob{
		PipelineRunID: runID,
		TargetID:      targetID,
		Payload:       mustMarshal(t, samplePayload()),
		State:         models.DispatchJobPending,
	}); err != nil {
		t.Fatal(err)
	}
	jobID := store.all()[0].ID

	for i := 0; i < MaxDispatchAttempts; i++ {
		if err := svc.tryDeliver(context.Background(), jobID); err == nil {
			t.Fatalf("tryDeliver should fail while dispatcher down (attempt %d)", i)
		}
	}

	j := store.mustGet(t, jobID)
	if j.State != models.DispatchJobDead {
		t.Fatalf("after %d attempts job should be dead, got %s (attempts=%d)", MaxDispatchAttempts, j.State, j.Attempts)
	}
	// A dead job is a no-op on further delivery.
	if err := svc.tryDeliver(context.Background(), jobID); err != nil {
		t.Fatalf("dead job tryDeliver should be a no-op, got %v", err)
	}
}

// TestSweepPendingRetriesDueJobs: the background sweeper delivers failed jobs
// whose backoff has elapsed once the Runner is reachable.
func TestSweepPendingRetriesDueJobs(t *testing.T) {
	store := newFakeDispatchStore()
	disp := &fakeDispatcher{fail: false}
	svc := newTestService(disp, store)

	runID, targetID := uuid.New(), uuid.New()
	if err := store.Create(&models.DispatchJob{
		PipelineRunID: runID,
		TargetID:      targetID,
		Payload:       mustMarshal(t, samplePayload()),
		State:         models.DispatchJobFailed,
		Attempts:      3,
		LastError:     "boom",
		NextRetryAt:   timePtr(time.Now().Add(-time.Second)),
	}); err != nil {
		t.Fatal(err)
	}
	jobID := store.all()[0].ID

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { svc.SweepPending(ctx); close(done) }()

	deadline := time.Now().Add(2 * time.Second)
	for {
		j := store.mustGet(t, jobID)
		if j.State == models.DispatchJobDispatched {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("sweeper did not dispatch due job in time, state=%s", j.State)
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	<-done

	if disp.count() != 1 {
		t.Fatalf("expected 1 dispatch from sweeper, got %d", disp.count())
	}
}
