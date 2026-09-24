package service

// AgentOpService 单测：枚举守卫、target 必填、创建即 queued；§16.5 扩展 ——
// 状态流转守卫（409 / 终态不可变）、创建即派发、离线留守、connect 排水。

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/rouroumaibing/software-distribution-platform-hub/internal/common"
	"github.com/rouroumaibing/software-distribution-platform-hub/internal/target/models"
)

type fakeAgentOpRepo struct {
	created  []*models.AgentOp
	ops      map[uuid.UUID]*models.AgentOp
	logs     map[uuid.UUID][]*models.AgentOpLog
	queued   []*models.AgentOp // returned by ListQueuedByTarget
	statuses []string          // record of UpdateStatus (from→to)
}

func newFakeAgentOpRepo() *fakeAgentOpRepo {
	return &fakeAgentOpRepo{
		ops:  map[uuid.UUID]*models.AgentOp{},
		logs: map[uuid.UUID][]*models.AgentOpLog{},
	}
}

func (f *fakeAgentOpRepo) Create(op *models.AgentOp) error {
	f.created = append(f.created, op)
	f.ops[op.ID] = op
	return nil
}

func (f *fakeAgentOpRepo) GetByID(id uuid.UUID) (*models.AgentOp, error) {
	if op, ok := f.ops[id]; ok {
		return op, nil
	}
	return nil, common.ErrNotFound.WithError(common.ErrResourceNotFound)
}

func (f *fakeAgentOpRepo) ListByTarget(uuid.UUID, common.Pagination) ([]models.AgentOp, int64, error) {
	return nil, 0, nil
}

func (f *fakeAgentOpRepo) UpdateStatus(id uuid.UUID, from, to, message string) error {
	f.statuses = append(f.statuses, from+"→"+to)
	f.ops[id].Status = to
	f.ops[id].Message = message
	return nil
}

func (f *fakeAgentOpRepo) AppendLog(opID uuid.UUID, stream, chunk string) (*models.AgentOpLog, error) {
	row := &models.AgentOpLog{OpID: opID, Seq: len(f.logs[opID]) + 1, Stream: stream, Chunk: chunk}
	f.logs[opID] = append(f.logs[opID], row)
	return row, nil
}

func (f *fakeAgentOpRepo) ListLogs(opID uuid.UUID) ([]models.AgentOpLog, error) {
	out := make([]models.AgentOpLog, 0, len(f.logs[opID]))
	for _, l := range f.logs[opID] {
		out = append(out, *l)
	}
	return out, nil
}

func (f *fakeAgentOpRepo) ListQueuedByTarget(uuid.UUID) ([]models.AgentOp, error) {
	out := make([]models.AgentOp, 0, len(f.queued))
	for _, op := range f.queued {
		out = append(out, *op)
	}
	return out, nil
}

type fakeDispatcher struct {
	dispatched []*models.AgentOp
	err        error
}

func (f *fakeDispatcher) Dispatch(op *models.AgentOp) error {
	if f.err != nil {
		return f.err
	}
	f.dispatched = append(f.dispatched, op)
	return nil
}

func TestAgentOpCreate_RejectsUnknownType(t *testing.T) {
	svc := NewAgentOpService(newFakeAgentOpRepo())
	err := svc.Create(&models.AgentOp{TargetID: uuid.New(), OpType: "reboot"})
	if err == nil || !strings.Contains(err.Error(), "unknown agent op type") {
		t.Fatalf("unknown type must be rejected, got %v", err)
	}
}

func TestAgentOpCreate_RequiresTarget(t *testing.T) {
	svc := NewAgentOpService(newFakeAgentOpRepo())
	err := svc.Create(&models.AgentOp{OpType: models.AgentOpInstall})
	if err == nil || !strings.Contains(err.Error(), "target") {
		t.Fatalf("missing target must be rejected, got %v", err)
	}
}

func TestAgentOpCreate_ForcesQueued(t *testing.T) {
	repo := newFakeAgentOpRepo()
	svc := NewAgentOpService(repo)
	op := &models.AgentOp{ID: uuid.New(), TargetID: uuid.New(), OpType: models.AgentOpUpgrade, Status: "running"}
	if err := svc.Create(op); err != nil {
		t.Fatalf("create: %v", err)
	}
	if op.Status != models.AgentOpQueued {
		t.Fatalf("status must be forced to queued, got %q", op.Status)
	}
	if len(repo.created) != 1 {
		t.Fatalf("op must be persisted once, got %d", len(repo.created))
	}
}

// §16.5：exec op 创建即派发；派发失败（离线）不影响 202 结果。
func TestAgentOpCreate_DispatchesExec(t *testing.T) {
	repo := newFakeAgentOpRepo()
	disp := &fakeDispatcher{}
	svc := NewAgentOpService(repo)
	svc.SetDispatcher(disp)

	op := &models.AgentOp{ID: uuid.New(), TargetID: uuid.New(), OpType: models.AgentOpExec}
	if err := svc.Create(op); err != nil {
		t.Fatalf("create: %v", err)
	}
	if len(disp.dispatched) != 1 {
		t.Fatalf("exec op must be dispatched once, got %d", len(disp.dispatched))
	}

	// 离线（ErrNoRunner 语义 = 任意 error）：op 仍然落库、不报错。
	dispErr := &fakeDispatcher{err: context.DeadlineExceeded}
	svc2 := NewAgentOpService(newFakeAgentOpRepo())
	svc2.SetDispatcher(dispErr)
	if err := svc2.Create(&models.AgentOp{ID: uuid.New(), TargetID: uuid.New(), OpType: models.AgentOpExec}); err != nil {
		t.Fatalf("dispatch failure must not fail create: %v", err)
	}
}

// §16.5 裁定：install/upgrade 不派发（bootstrap 流程独立特性）。
func TestAgentOpCreate_InstallUpgradeNotDispatched(t *testing.T) {
	disp := &fakeDispatcher{}
	svc := NewAgentOpService(newFakeAgentOpRepo())
	svc.SetDispatcher(disp)
	for _, typ := range []string{models.AgentOpInstall, models.AgentOpUpgrade} {
		if err := svc.Create(&models.AgentOp{ID: uuid.New(), TargetID: uuid.New(), OpType: typ}); err != nil {
			t.Fatalf("create %s: %v", typ, err)
		}
	}
	if len(disp.dispatched) != 0 {
		t.Fatalf("install/upgrade must stay queued, dispatched %d", len(disp.dispatched))
	}
}

func TestAgentOpApplyStatus_TransitionGuard(t *testing.T) {
	id := uuid.New()
	repo := newFakeAgentOpRepo()
	repo.ops[id] = &models.AgentOp{ID: id, TargetID: uuid.New(), OpType: models.AgentOpExec, Status: models.AgentOpQueued}
	svc := NewAgentOpService(repo)

	// queued→running 合法。
	op, err := svc.ApplyStatus(context.Background(), id, models.AgentOpRunning, "")
	if err != nil || op.Status != models.AgentOpRunning {
		t.Fatalf("queued→running must pass, got %v / %q", err, opStatus(op))
	}
	// running→succeeded 合法，终态落定。
	op, err = svc.ApplyStatus(context.Background(), id, models.AgentOpSucceeded, "done")
	if err != nil || op.Status != models.AgentOpSucceeded {
		t.Fatalf("running→succeeded must pass, got %v / %q", err, opStatus(op))
	}
	// 终态再流转 → 409 冲突。
	_, err = svc.ApplyStatus(context.Background(), id, models.AgentOpFailed, "")
	ae, ok := err.(*common.APIError)
	if !ok || ae.Code != 409 {
		t.Fatalf("terminal op must reject with 409, got %v", err)
	}
	// 未知状态 → 400。
	if _, err := svc.ApplyStatus(context.Background(), id, "zombie", ""); err == nil || !strings.Contains(err.Error(), "unknown agent op status") {
		t.Fatalf("unknown status must 400, got %v", err)
	}
	// 不存在的 op → 404。
	if _, err := svc.ApplyStatus(context.Background(), uuid.New(), models.AgentOpRunning, ""); err == nil {
		t.Fatal("missing op must 404")
	}
}

// §16.5：日志落库 + SSE 扇出；订阅者收到 status/log 事件。
func TestAgentOpAppendLog_PersistsAndPublishes(t *testing.T) {
	id := uuid.New()
	repo := newFakeAgentOpRepo()
	repo.ops[id] = &models.AgentOp{ID: id, TargetID: uuid.New(), OpType: models.AgentOpExec, Status: models.AgentOpRunning}
	stream := NewOpStream()
	svc := NewAgentOpService(repo)
	svc.SetStream(stream)

	events, cancel := stream.Subscribe(id)
	defer cancel()

	if _, err := svc.AppendLog(context.Background(), id, "stdout", "hello"); err != nil {
		t.Fatalf("append log: %v", err)
	}
	ev := <-events
	if ev.Kind != OpEventLog || ev.Log == nil || ev.Log.Chunk != "hello" || ev.Log.Seq != 1 {
		t.Fatalf("subscriber must receive log event seq=1, got %+v", ev)
	}

	// 未知 op → 404（防止恶意 runner 刷孤儿日志行）。
	if _, err := svc.AppendLog(context.Background(), uuid.New(), "stdout", "x"); err == nil {
		t.Fatal("log for missing op must 404")
	}
}

// §16.5：runner 重连时排水 —— queued exec op 全部补派发。
func TestAgentOpDrainTarget_RedispatchesQueued(t *testing.T) {
	repo := newFakeAgentOpRepo()
	disp := &fakeDispatcher{}
	svc := NewAgentOpService(repo)
	svc.SetDispatcher(disp)

	a, b := &models.AgentOp{ID: uuid.New(), TargetID: uuid.New(), OpType: models.AgentOpExec},
		&models.AgentOp{ID: uuid.New(), TargetID: uuid.New(), OpType: models.AgentOpExec}
	repo.queued = []*models.AgentOp{a, b}
	if err := svc.DrainTarget(context.Background(), uuid.New()); err != nil {
		t.Fatalf("drain: %v", err)
	}
	if len(disp.dispatched) != 2 {
		t.Fatalf("drain must redispatch all queued exec ops, got %d", len(disp.dispatched))
	}
}

func opStatus(op *models.AgentOp) string {
	if op == nil {
		return "<nil>"
	}
	return op.Status
}
