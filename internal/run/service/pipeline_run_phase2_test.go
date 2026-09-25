package service

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"
	"gorm.io/gorm"

	runnerapi "github.com/rouroumaibing/software-distribution-platform-runner/api/v1alpha1"

	"github.com/rouroumaibing/software-distribution-platform-hub/internal/common"
	pipelinemodels "github.com/rouroumaibing/software-distribution-platform-hub/internal/pipeline/models"
	"github.com/rouroumaibing/software-distribution-platform-hub/internal/run/models"
	targetmodels "github.com/rouroumaibing/software-distribution-platform-hub/internal/target/models"
)

// ---- in-memory fakes for the repository interfaces (no Postgres needed) ----

type fakePipelineRunStore struct {
	mu   sync.Mutex
	runs map[uuid.UUID]*models.PipelineRun
}

func newFakePipelineRunStore() *fakePipelineRunStore {
	return &fakePipelineRunStore{runs: map[uuid.UUID]*models.PipelineRun{}}
}

func (f *fakePipelineRunStore) Create(r *models.PipelineRun) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if r.ID == uuid.Nil {
		r.ID = uuid.New()
	}
	cp := *r
	f.runs[cp.ID] = &cp
	return nil
}

func (f *fakePipelineRunStore) GetByID(id uuid.UUID) (*models.PipelineRun, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	r, ok := f.runs[id]
	if !ok {
		return nil, gorm.ErrRecordNotFound
	}
	cp := *r
	return &cp, nil
}

func (f *fakePipelineRunStore) Update(r *models.PipelineRun) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	cp := *r
	f.runs[cp.ID] = &cp
	return nil
}

func (f *fakePipelineRunStore) GetByCRNameTarget(crName string, targetID uuid.UUID) (*models.PipelineRun, error) {
	return nil, gorm.ErrRecordNotFound
}

func (f *fakePipelineRunStore) FindByPipelineID(pipelineID uuid.UUID, p common.Pagination) ([]models.PipelineRun, int64, error) {
	return nil, 0, nil
}

func (f *fakePipelineRunStore) FindAll(p common.Pagination, phase string, componentID uuid.UUID, createdAfter *time.Time) ([]models.PipelineRun, int64, error) {
	return nil, 0, nil
}

type fakeTaskRunStore struct{}

func (fakeTaskRunStore) Upsert(*models.TaskRun) error { return nil }
func (fakeTaskRunStore) SaveStatus(uuid.UUID, runnerapi.TaskRunStatusSummary) error {
	return nil
}
func (fakeTaskRunStore) GetByRunAndTaskName(uuid.UUID, string) (*models.TaskRun, error) {
	return nil, gorm.ErrRecordNotFound
}
func (fakeTaskRunStore) ListByPipelineRunID(uuid.UUID) ([]models.TaskRun, error) {
	return nil, nil
}

type fakePipelineDefStore struct{ version int }

func (f fakePipelineDefStore) GetByID(uuid.UUID) (*pipelinemodels.Pipeline, error) {
	return &pipelinemodels.Pipeline{Name: "p", Version: f.version, ComponentID: uuid.New()}, nil
}

type fakeStageStore struct{}

func (fakeStageStore) ListByPipelineID(uuid.UUID) ([]pipelinemodels.PipelineStage, error) {
	return []pipelinemodels.PipelineStage{{Name: "stage-1", Sequence: 1}}, nil
}

type fakeTaskTemplateStore struct{}

func (fakeTaskTemplateStore) ListByStageID(uuid.UUID) ([]pipelinemodels.PipelineTaskTemplate, error) {
	return []pipelinemodels.PipelineTaskTemplate{{
		Name:        "task-1",
		Type:        runnerapi.TaskTypeBuild,
		ScriptArgs:  datatypes.JSON("[]"),
		Produces:    datatypes.JSON("[]"),
		Consumes:    datatypes.JSON("[]"),
		RetryPolicy: datatypes.JSON(`{"maxRetries":0}`),
	}}, nil
}

type fakeTargetStore struct{ status string }

func (f fakeTargetStore) Get(id uuid.UUID) (*targetmodels.Target, error) {
	// 真实 repo 会把整行（含主键）读回来；fake 必须同样回填 ID，否则
	// selectTarget 会返回零值 UUID，让"已解析出的 target"在下游变成空 —— 生产强
	// 审批（B-11）按 target 判生产环境，就会因为这个假阴性被静默跳过。
	return &targetmodels.Target{ID: id, Status: f.status}, nil
}
func (f fakeTargetStore) List(p common.Pagination) ([]targetmodels.Target, int64, error) {
	return nil, 0, nil
}

// newFullTestService wires the service with in-memory fakes so the full
// Trigger (fan-out) / Redispatch paths run without a database.
func newFullTestService(disp Dispatcher, store DispatchJobStore) *PipelineRunService {
	return &PipelineRunService{
		repo:             newFakePipelineRunStore(),
		taskRepo:         fakeTaskRunStore{},
		pipelineRepo:     fakePipelineDefStore{version: 1},
		stageRepo:        fakeStageStore{},
		taskTemplateRepo: fakeTaskTemplateStore{},
		targetSvc:        fakeTargetStore{status: targetmodels.TargetStatusOnline},
		dispatcher:       disp,
		dispatchRepo:     store,
		sweepInterval:    20 * time.Millisecond,
	}
}

// TestTriggerFanoutCreatesOneRunPerTarget: a trigger with TargetIDs fans
// out to one independent run per environment, each delivered to its target.
func TestTriggerFanoutCreatesOneRunPerTarget(t *testing.T) {
	store := newFakeDispatchStore()
	disp := &fakeDispatcher{fail: false}
	svc := newFullTestService(disp, store)

	c1, c2 := uuid.New(), uuid.New()
	runs, err := svc.Trigger(uuid.New(), &models.TriggerRequest{
		TargetIDs:   []uuid.UUID{c1, c2},
		TriggeredBy: "tester",
	})
	if err != nil {
		t.Fatalf("fanout trigger: %v", err)
	}
	if len(runs) != 2 {
		t.Fatalf("expected 2 runs, got %d", len(runs))
	}
	jobs := store.all()
	if len(jobs) != 2 {
		t.Fatalf("expected 2 dispatch jobs, got %d", len(jobs))
	}
	if disp.count() != 2 {
		t.Fatalf("expected 2 dispatches (one per target), got %d", disp.count())
	}
	seen := map[uuid.UUID]bool{}
	for _, r := range runs {
		seen[r.TargetID] = true
	}
	if !seen[c1] || !seen[c2] {
		t.Fatalf("runs not assigned to both targets: %v", runs)
	}
	for _, j := range jobs {
		if j.State != models.DispatchJobDispatched {
			t.Fatalf("job %s should be dispatched, got %s", j.ID, j.State)
		}
	}
}

// TestTriggerFanoutOfflineTargetDoesNotFail: with the Runner offline, a
// fan-out trigger must NOT error — each run stays Pending and its job is
// queued for later redelivery (durable dispatch).
func TestTriggerFanoutOfflineTargetDoesNotFail(t *testing.T) {
	store := newFakeDispatchStore()
	disp := &fakeDispatcher{fail: true} // Runner down
	svc := newFullTestService(disp, store)

	c1, c2 := uuid.New(), uuid.New()
	runs, err := svc.Trigger(uuid.New(), &models.TriggerRequest{TargetIDs: []uuid.UUID{c1, c2}})
	if err != nil {
		t.Fatalf("fanout with offline targets must not fail: %v", err)
	}
	if len(runs) != 2 {
		t.Fatalf("expected 2 runs, got %d", len(runs))
	}
	for _, r := range runs {
		if r.Phase != runnerapi.PipelineRunPending {
			t.Fatalf("run %s should stay Pending when Runner offline, got %s", r.ID, r.Phase)
		}
	}
	jobs := store.all()
	if len(jobs) != 2 {
		t.Fatalf("expected 2 jobs, got %d", len(jobs))
	}
	for _, j := range jobs {
		if j.State == models.DispatchJobDispatched {
			t.Fatalf("offline job must not be dispatched: %s", j.ID)
		}
	}
	if disp.count() != 0 {
		t.Fatalf("dispatcher must not be called when offline, got %d", disp.count())
	}
}

// TestRedispatchDeadJobRetriesAndDelivers: a run whose dispatch job is dead is
// re-delivered via a fresh job carrying the same payload, and the run is reset
// from Failed to Pending.
func TestRedispatchDeadJobRetriesAndDelivers(t *testing.T) {
	store := newFakeDispatchStore()
	disp := &fakeDispatcher{fail: false}
	svc := newFullTestService(disp, store)

	runID, targetID := uuid.New(), uuid.New()
	if err := svc.repo.Create(&models.PipelineRun{ID: runID, TargetID: targetID, Phase: runnerapi.PipelineRunFailed}); err != nil {
		t.Fatal(err)
	}
	if err := store.Create(&models.DispatchJob{
		PipelineRunID: runID,
		TargetID:      targetID,
		Payload:       mustMarshal(t, samplePayload()),
		State:         models.DispatchJobDead,
		Attempts:      MaxDispatchAttempts,
	}); err != nil {
		t.Fatal(err)
	}

	job, err := svc.Redispatch(context.Background(), runID)
	if err != nil {
		t.Fatalf("redispatch: %v", err)
	}
	if job.State != models.DispatchJobDispatched {
		t.Fatalf("redispatch job should be dispatched, got %s", job.State)
	}
	if disp.count() != 1 {
		t.Fatalf("expected 1 dispatch from redispatch, got %d", disp.count())
	}
	r, err := svc.repo.GetByID(runID)
	if err != nil {
		t.Fatal(err)
	}
	if r.Phase != runnerapi.PipelineRunPending {
		t.Fatalf("run should reset to Pending, got %s", r.Phase)
	}
}

// TestRedispatchOfflineStaysPending: when the Runner is offline, Redispatch
// still succeeds by enqueuing a pending job; delivery happens on reconnect.
func TestRedispatchOfflineStaysPending(t *testing.T) {
	store := newFakeDispatchStore()
	disp := &fakeDispatcher{fail: true}
	svc := newFullTestService(disp, store)

	runID, targetID := uuid.New(), uuid.New()
	if err := svc.repo.Create(&models.PipelineRun{ID: runID, TargetID: targetID, Phase: runnerapi.PipelineRunFailed}); err != nil {
		t.Fatal(err)
	}
	if err := store.Create(&models.DispatchJob{
		PipelineRunID: runID,
		TargetID:      targetID,
		Payload:       mustMarshal(t, samplePayload()),
		State:         models.DispatchJobFailed,
		Attempts:      3,
	}); err != nil {
		t.Fatal(err)
	}

	job, err := svc.Redispatch(context.Background(), runID)
	if err != nil {
		t.Fatalf("redispatch: %v", err)
	}
	if job.State == models.DispatchJobDispatched {
		t.Fatalf("offline redispatch must stay pending, got %s", job.State)
	}
	if disp.count() != 0 {
		t.Fatalf("dispatcher must not be called when offline, got %d", disp.count())
	}
	r, _ := svc.repo.GetByID(runID)
	if r.Phase != runnerapi.PipelineRunPending {
		t.Fatalf("run should reset to Pending, got %s", r.Phase)
	}
}

// TestRedispatchNoDispatchRecord: a run with no dispatch history can't be
// redelivered; the caller should re-trigger the pipeline instead.
func TestRedispatchNoDispatchRecord(t *testing.T) {
	store := newFakeDispatchStore()
	disp := &fakeDispatcher{}
	svc := newFullTestService(disp, store)

	runID := uuid.New()
	if err := svc.repo.Create(&models.PipelineRun{ID: runID, Phase: runnerapi.PipelineRunPending}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Redispatch(context.Background(), runID); err == nil {
		t.Fatal("expected error when run has no dispatch record")
	}
}
