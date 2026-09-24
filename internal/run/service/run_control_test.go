package service

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"gorm.io/gorm"

	runnerapi "github.com/rouroumaibing/software-distribution-platform-runner/api/v1alpha1"

	"github.com/rouroumaibing/software-distribution-platform-hub/internal/gateway"
	"github.com/rouroumaibing/software-distribution-platform-hub/internal/run/models"
)

// controlDispatcher records the rerun / cancel payloads the service pushes and
// can be toggled offline to exercise the ErrNoRunner branch.
type controlDispatcher struct {
	rerun  *runnerapi.RerunTaskPayload
	cancel *runnerapi.CancelPipelineRunPayload
	fail   error
}

func (d *controlDispatcher) Dispatch(context.Context, uuid.UUID, *runnerapi.ApplyPipelineRunPayload) error {
	return nil
}
func (d *controlDispatcher) Approve(context.Context, uuid.UUID, *runnerapi.ApproveTaskPayload) error {
	return nil
}
func (d *controlDispatcher) RolloutControl(context.Context, uuid.UUID, *runnerapi.RolloutControlPayload) error {
	return nil
}
func (d *controlDispatcher) RerunTask(_ context.Context, _ uuid.UUID, p *runnerapi.RerunTaskPayload) error {
	if d.fail != nil {
		return d.fail
	}
	d.rerun = p
	return nil
}
func (d *controlDispatcher) CancelPipelineRun(_ context.Context, _ uuid.UUID, p *runnerapi.CancelPipelineRunPayload) error {
	if d.fail != nil {
		return d.fail
	}
	d.cancel = p
	return nil
}

// taskRunStoreWithTask returns a TaskRun for any name so the rerun precondition
// (task belongs to the run) is satisfied.
type taskRunStoreWithTask struct{}

func (taskRunStoreWithTask) Upsert(*models.TaskRun) error { return nil }
func (taskRunStoreWithTask) SaveStatus(uuid.UUID, runnerapi.TaskRunStatusSummary) error {
	return nil
}
func (taskRunStoreWithTask) GetByRunAndTaskName(_ uuid.UUID, name string) (*models.TaskRun, error) {
	return &models.TaskRun{TaskName: name, Type: runnerapi.TaskTypeBuild}, nil
}
func (taskRunStoreWithTask) ListByPipelineRunID(uuid.UUID) ([]models.TaskRun, error) {
	return nil, nil
}

// controlService wires the in-memory run store with a recording dispatcher and
// a task store that resolves any task name.
func controlService(disp Dispatcher, run *models.PipelineRun) *PipelineRunService {
	rs := newFakePipelineRunStore()
	if run != nil {
		_ = rs.Create(run)
	}
	return &PipelineRunService{
		repo:         rs,
		taskRepo:     taskRunStoreWithTask{},
		dispatcher:   disp,
		dispatchRepo: newFakeDispatchStore(),
	}
}

func TestCancelRunDispatchesCommand(t *testing.T) {
	disp := &controlDispatcher{}
	run := &models.PipelineRun{
		ID:          uuid.New(),
		TargetID:    uuid.New(),
		CRName:      "pr-1",
		CRNamespace: "sdp-runs",
		Phase:       runnerapi.PipelineRunRunning,
	}
	svc := controlService(disp, run)

	if err := svc.CancelRun(context.Background(), run.ID, "alice"); err != nil {
		t.Fatalf("CancelRun: %v", err)
	}
	if disp.cancel == nil {
		t.Fatal("cancel was not dispatched")
	}
	if disp.cancel.PipelineRunName != "pr-1" || disp.cancel.Namespace != "sdp-runs" {
		t.Errorf("payload = %+v, want name pr-1 ns sdp-runs", disp.cancel)
	}
	if disp.cancel.Operator != "alice" {
		t.Errorf("operator = %q, want alice", disp.cancel.Operator)
	}
}

func TestCancelRunRejectsTerminalRun(t *testing.T) {
	for _, phase := range []runnerapi.PipelineRunPhase{
		runnerapi.PipelineRunSucceeded,
		runnerapi.PipelineRunFailed,
		runnerapi.PipelineRunCancelled,
	} {
		disp := &controlDispatcher{}
		run := &models.PipelineRun{ID: uuid.New(), TargetID: uuid.New(), CRName: "pr", CRNamespace: "ns", Phase: phase}
		svc := controlService(disp, run)

		err := svc.CancelRun(context.Background(), run.ID, "alice")
		if !errors.Is(err, ErrRunTerminal) {
			t.Fatalf("phase %s: err = %v, want ErrRunTerminal", phase, err)
		}
		if disp.cancel != nil {
			t.Errorf("phase %s: a terminal run must not dispatch a cancel", phase)
		}
	}
}

func TestCancelRunOfflineTarget(t *testing.T) {
	disp := &controlDispatcher{fail: gateway.ErrNoRunner}
	run := &models.PipelineRun{ID: uuid.New(), TargetID: uuid.New(), CRName: "pr", CRNamespace: "ns", Phase: runnerapi.PipelineRunRunning}
	svc := controlService(disp, run)

	err := svc.CancelRun(context.Background(), run.ID, "alice")
	if err == nil || !errors.Is(err, gateway.ErrNoRunner) {
		t.Fatalf("err = %v, want wrapped ErrNoRunner", err)
	}
}

func TestRerunTaskDispatchesCommand(t *testing.T) {
	disp := &controlDispatcher{}
	run := &models.PipelineRun{ID: uuid.New(), TargetID: uuid.New(), CRName: "pr-9", Phase: runnerapi.PipelineRunFailed}
	svc := controlService(disp, run)

	if err := svc.RerunTask(context.Background(), run.ID, "build", "bob"); err != nil {
		t.Fatalf("RerunTask: %v", err)
	}
	if disp.rerun == nil {
		t.Fatal("rerun was not dispatched")
	}
	if disp.rerun.PipelineRunName != "pr-9" || disp.rerun.TaskName != "build" || disp.rerun.Operator != "bob" {
		t.Errorf("payload = %+v", disp.rerun)
	}
}

func TestRerunTaskRejectsUnknownTask(t *testing.T) {
	svc := &PipelineRunService{
		repo:       newFakePipelineRunStore(),
		taskRepo:   fakeTaskRunStore{}, // resolves nothing -> gorm.ErrRecordNotFound
		dispatcher: &controlDispatcher{},
	}
	run := &models.PipelineRun{ID: uuid.New(), TargetID: uuid.New(), CRName: "pr"}
	_ = svc.repo.Create(run)

	err := svc.RerunTask(context.Background(), run.ID, "ghost", "bob")
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("err = %v, want wrapped gorm.ErrRecordNotFound", err)
	}
}
