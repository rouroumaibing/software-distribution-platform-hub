package service

// 灰度回流单测（RUNNER-REFLUX-SPEC §3 / STATUS #8）：ApplyStatus 收到带
// Rollout 快照的任务摘要 → rolloutStore.UpsertSnapshot 以正确权重被调用；
// 无快照 → 不调用；store 失败 → 状态回写不受影响（best-effort）。

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	runnerapi "github.com/rouroumaibing/software-distribution-platform-runner/api/v1alpha1"

	"github.com/rouroumaibing/software-distribution-platform-hub/internal/run/models"
)

type recordingRolloutStore struct {
	calls      int
	lastID     uuid.UUID
	lastPhase  runnerapi.RolloutPhase
	lastWeight int
	fail       error
}

func (r *recordingRolloutStore) UpsertSnapshot(taskRunID uuid.UUID, ts runnerapi.RolloutStatusSummary, _ time.Time) error {
	if r.fail != nil {
		return r.fail
	}
	r.calls++
	r.lastID = taskRunID
	r.lastPhase = ts.Phase
	r.lastWeight = ts.CurrentWeight
	return nil
}

// runStoreByCR makes GetByCRNameTarget resolve the given run so ApplyStatus
// can find it (the phase2 fake returns not-found for everything).
type runStoreByCR struct {
	*fakePipelineRunStore
	run *models.PipelineRun
}

func (f *runStoreByCR) GetByCRNameTarget(crName string, targetID uuid.UUID) (*models.PipelineRun, error) {
	if crName == f.run.CRName && targetID == f.run.TargetID {
		return f.run, nil
	}
	return nil, gorm.ErrRecordNotFound
}

func refluxService(run *models.PipelineRun, store RolloutSnapshotStore) *PipelineRunService {
	rs := &runStoreByCR{fakePipelineRunStore: newFakePipelineRunStore(), run: run}
	_ = rs.Create(run)
	svc := &PipelineRunService{
		repo:         rs,
		taskRepo:     taskRunStoreWithTask{},
		dispatchRepo: newFakeDispatchStore(),
	}
	if store != nil {
		svc.SetRolloutStore(store)
	}
	return svc
}

func refluxPayload(run *models.PipelineRun, weight int, withRollout bool) *runnerapi.StatusUpdatePayload {
	ts := runnerapi.TaskRunStatusSummary{
		Name:  "deploy-canary",
		Phase: runnerapi.TaskRunRunning,
	}
	if withRollout {
		ts.Rollout = &runnerapi.RolloutStatusSummary{
			Phase:         runnerapi.RolloutProgressing,
			CurrentWeight: weight,
		}
	}
	return &runnerapi.StatusUpdatePayload{
		PipelineRunName:      run.CRName,
		PipelineRunNamespace: run.CRNamespace,
		Phase:                runnerapi.PipelineRunRunning,
		Tasks:                []runnerapi.TaskRunStatusSummary{ts},
	}
}

func TestApplyStatus_ReflexRolloutSnapshotUpserts(t *testing.T) {
	run := &models.PipelineRun{ID: uuid.New(), TargetID: uuid.New(), CRName: "pr-ro", CRNamespace: "ns", Phase: runnerapi.PipelineRunRunning}
	store := &recordingRolloutStore{}
	svc := refluxService(run, store)

	if err := svc.ApplyStatus(context.Background(), run.TargetID, refluxPayload(run, 30, true)); err != nil {
		t.Fatalf("ApplyStatus: %v", err)
	}
	if store.calls != 1 {
		t.Fatalf("UpsertSnapshot calls = %d, want 1", store.calls)
	}
	if store.lastWeight != 30 || store.lastPhase != runnerapi.RolloutProgressing {
		t.Fatalf("snapshot = (phase %s, weight %d), want (Progressing, 30)", store.lastPhase, store.lastWeight)
	}
}

func TestApplyStatus_NoRolloutFieldSkipsStore(t *testing.T) {
	run := &models.PipelineRun{ID: uuid.New(), TargetID: uuid.New(), CRName: "pr-noro", CRNamespace: "ns", Phase: runnerapi.PipelineRunRunning}
	store := &recordingRolloutStore{}
	svc := refluxService(run, store)

	if err := svc.ApplyStatus(context.Background(), run.TargetID, refluxPayload(run, 0, false)); err != nil {
		t.Fatalf("ApplyStatus: %v", err)
	}
	if store.calls != 0 {
		t.Fatalf("UpsertSnapshot calls = %d, want 0（非 Deploy 任务不带快照）", store.calls)
	}
}

func TestApplyStatus_RolloutStoreFailureDoesNotBreakStatusWrite(t *testing.T) {
	run := &models.PipelineRun{ID: uuid.New(), TargetID: uuid.New(), CRName: "pr-fail", CRNamespace: "ns", Phase: runnerapi.PipelineRunRunning}
	store := &recordingRolloutStore{fail: errors.New("db down")}
	svc := refluxService(run, store)

	if err := svc.ApplyStatus(context.Background(), run.TargetID, refluxPayload(run, 50, true)); err != nil {
		t.Fatalf("快照落库失败必须不影响状态回写，got %v", err)
	}
	if run.Phase != runnerapi.PipelineRunRunning {
		t.Fatalf("run phase = %q, want Running", run.Phase)
	}
}
