package service

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"

	runnerapi "github.com/rouroumaibing/software-distribution-platform-runner/api/v1alpha1"

	"github.com/rouroumaibing/software-distribution-platform-hub/internal/common"
	"github.com/rouroumaibing/software-distribution-platform-hub/internal/gateway"
	pipelinemodels "github.com/rouroumaibing/software-distribution-platform-hub/internal/pipeline/models"
	"github.com/rouroumaibing/software-distribution-platform-hub/internal/run/models"
	targetmodels "github.com/rouroumaibing/software-distribution-platform-hub/internal/target/models"
)

// 覆盖 backlog B-11 的两个子项（全部内存 fake，无需 Postgres / 集群）：
//   - 审批超时：超时未决策的审批自动拒绝，而不是无限期挂起（Epic 8）；
//   - 生产强审批：向生产环境触发的运行必须带人工审核阶段（平台级策略）。

// ---- 审批超时：纯谓词 ------------------------------------------------------

func TestApprovalExpired(t *testing.T) {
	created := time.Unix(1_700_000_000, 0)
	cases := []struct {
		name    string
		timeout int
		now     time.Time
		want    bool
	}{
		{"未配置超时 = 无限期等待，永不超时", 0, created.Add(1000 * time.Hour), false},
		{"负数同样视为未配置", -5, created.Add(1000 * time.Hour), false},
		{"恰好到时限不算超时（严格大于，免临界抖动）", 60, created.Add(60 * time.Second), false},
		{"超过时限 1 秒即超时", 60, created.Add(61 * time.Second), true},
		{"还差 1 秒不超时", 60, created.Add(59 * time.Second), false},
		{"创建时间在未来（时钟回拨）不超时", 60, created.Add(-time.Hour), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ApprovalExpired(created, tc.timeout, tc.now); got != tc.want {
				t.Fatalf("want %v, got %v", tc.want, got)
			}
		})
	}
}

func TestHasApprovalGate(t *testing.T) {
	if HasApprovalGate(nil) {
		t.Fatalf("no tasks = no gate")
	}
	if HasApprovalGate([]runnerapi.PipelineTaskSpec{{Type: runnerapi.TaskTypeBuild}}) {
		t.Fatalf("build tasks are not an approval gate")
	}
	if !HasApprovalGate([]runnerapi.PipelineTaskSpec{
		{Type: runnerapi.TaskTypeBuild},
		{Type: runnerapi.TaskTypeApproval},
	}) {
		t.Fatalf("one approval task is a gate")
	}
}

// ---- 审批超时：编排 --------------------------------------------------------

type stubApprovalStore struct {
	pending []models.PipelineApproval
	updated []models.PipelineApproval
}

func (s *stubApprovalStore) Create(*models.PipelineApproval) error { return nil }
func (s *stubApprovalStore) GetByRunAndTask(uuid.UUID, uuid.UUID) (*models.PipelineApproval, error) {
	return nil, errors.New("not used")
}
func (s *stubApprovalStore) Update(a *models.PipelineApproval) error {
	cp := *a
	s.updated = append(s.updated, cp)
	return nil
}
func (s *stubApprovalStore) ListByRun(uuid.UUID) ([]models.PipelineApproval, error) { return nil, nil }
func (s *stubApprovalStore) ListPending(int) ([]models.PipelineApproval, error) {
	return s.pending, nil
}

type stubTaskRunStore struct {
	taskName string
	taskID   uuid.UUID
}

func (stubTaskRunStore) Upsert(*models.TaskRun) error                               { return nil }
func (stubTaskRunStore) SaveStatus(uuid.UUID, runnerapi.TaskRunStatusSummary) error { return nil }
func (stubTaskRunStore) GetByRunAndTaskName(uuid.UUID, string) (*models.TaskRun, error) {
	return nil, errors.New("not used")
}
func (s stubTaskRunStore) ListByPipelineRunID(uuid.UUID) ([]models.TaskRun, error) {
	return []models.TaskRun{{ID: s.taskID, TaskName: s.taskName, Type: runnerapi.TaskTypeApproval, Phase: runnerapi.TaskRunRunning}}, nil
}

type stubStageStore struct{ id uuid.UUID }

func (s stubStageStore) ListByPipelineID(uuid.UUID) ([]pipelinemodels.PipelineStage, error) {
	return []pipelinemodels.PipelineStage{{ID: s.id, Name: "审批", Sequence: 1}}, nil
}

type stubTemplateStore struct{ timeout int }

func (s stubTemplateStore) ListByStageID(uuid.UUID) ([]pipelinemodels.PipelineTaskTemplate, error) {
	cfg, _ := jsonMarshalApprovalConfig(int64(s.timeout))
	return []pipelinemodels.PipelineTaskTemplate{{
		Name:           "deploy-prod",
		Type:           runnerapi.TaskTypeApproval,
		ApprovalConfig: cfg,
	}}, nil
}

func jsonMarshalApprovalConfig(timeout int64) (datatypes.JSON, error) {
	return datatypes.JSON([]byte(`{"requiredApprovals":1,"timeoutSeconds":` + itoa64(timeout) + `}`)), nil
}

func itoa64(n int64) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	if neg {
		return "-" + string(b)
	}
	return string(b)
}

// recordingDispatcher 记下 Approve 下发的载荷，并可切换为失败。
type recordingDispatcher struct {
	approvals  []*runnerapi.ApproveTaskPayload
	approveErr error
	targets    []uuid.UUID
}

func (d *recordingDispatcher) Dispatch(context.Context, uuid.UUID, *runnerapi.ApplyPipelineRunPayload) error {
	return nil
}
func (d *recordingDispatcher) Approve(_ context.Context, targetID uuid.UUID, p *runnerapi.ApproveTaskPayload) error {
	if d.approveErr != nil {
		return d.approveErr
	}
	d.approvals = append(d.approvals, p)
	d.targets = append(d.targets, targetID)
	return nil
}
func (d *recordingDispatcher) RolloutControl(context.Context, uuid.UUID, *runnerapi.RolloutControlPayload) error {
	return nil
}

// timeoutFixture 构造一个"某 run 有一条挂起审批、其任务声明了 timeout"的场景。
func timeoutFixture(t *testing.T, timeout int, age time.Duration, dispatchErr error) (*PipelineRunService, *stubApprovalStore, *recordingDispatcher) {
	t.Helper()
	runID := uuid.New()
	taskRunID := uuid.New()
	stageID := uuid.New()

	runs := newFakePipelineRunStore()
	run := &models.PipelineRun{
		PipelineID: uuid.New(),
		TargetID:   uuid.New(),
		CRName:     "pr-abc",
		Phase:      runnerapi.PipelineRunWaitingApproval,
	}
	run.ID = runID
	if err := runs.Create(run); err != nil {
		t.Fatalf("seed run: %v", err)
	}

	approvals := &stubApprovalStore{pending: []models.PipelineApproval{{
		RunID:       runID,
		TaskRunID:   &taskRunID,
		Status:      "Pending",
		RequestedBy: "alice",
		CreatedAt:   time.Now().Add(-age),
	}}}
	disp := &recordingDispatcher{approveErr: dispatchErr}

	return &PipelineRunService{
		repo:             runs,
		taskRepo:         stubTaskRunStore{taskName: "deploy-prod", taskID: taskRunID},
		pipelineRepo:     fakePipelineDefStore{version: 1},
		stageRepo:        stubStageStore{id: stageID},
		taskTemplateRepo: stubTemplateStore{timeout: timeout},
		targetSvc:        fakeTargetStore{status: targetmodels.TargetStatusOnline},
		dispatcher:       disp,
		dispatchRepo:     newFakeDispatchStore(),
		approvalRepo:     approvals,
	}, approvals, disp
}

func TestSweepApprovalTimeouts_AutoRejectsExpiredApproval(t *testing.T) {
	svc, approvals, disp := timeoutFixture(t, 60, 10*time.Minute, nil)

	closed, err := svc.SweepApprovalTimeouts(context.Background(), time.Now())
	if err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if closed != 1 {
		t.Fatalf("want 1 auto-closed approval, got %d", closed)
	}
	if len(disp.approvals) != 1 {
		t.Fatalf("want 1 dispatched rejection, got %d", len(disp.approvals))
	}
	p := disp.approvals[0]
	if !p.Rejected {
		t.Fatalf("timeout must auto-REJECT, got %+v", p)
	}
	if p.Approver != AutoTimeoutApprover {
		t.Fatalf("the actor must be identifiable as the system, got %q", p.Approver)
	}
	if p.TaskName != "deploy-prod" || p.PipelineRunName != "pr-abc" {
		t.Fatalf("payload must address the exact task/run, got %+v", p)
	}
	if len(approvals.updated) != 1 || approvals.updated[0].Status != "Cancelled" {
		t.Fatalf("hub-side record must be closed after a successful dispatch, got %+v", approvals.updated)
	}
	if approvals.updated[0].DecidedAt == nil {
		t.Fatalf("a closed approval must carry decidedAt (audit)")
	}
}

func TestSweepApprovalTimeouts_LeavesUnexpiredAlone(t *testing.T) {
	svc, approvals, disp := timeoutFixture(t, 3600, time.Minute, nil)

	closed, err := svc.SweepApprovalTimeouts(context.Background(), time.Now())
	if err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if closed != 0 || len(disp.approvals) != 0 || len(approvals.updated) != 0 {
		t.Fatalf("an unexpired approval must be untouched, closed=%d", closed)
	}
}

// timeoutSeconds=0 = 无限期等待（契约原文），绝不能被当成"立刻超时"整批关掉。
func TestSweepApprovalTimeouts_ZeroTimeoutWaitsForever(t *testing.T) {
	svc, _, disp := timeoutFixture(t, 0, 1000*time.Hour, nil)

	closed, err := svc.SweepApprovalTimeouts(context.Background(), time.Now())
	if err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if closed != 0 || len(disp.approvals) != 0 {
		t.Fatalf("a task without a timeout must never be auto-closed, closed=%d", closed)
	}
}

// 下发失败（target 离线）时**保持 Pending**：先改 hub 记录会在 runner 仍停着的情况下
// 留下两侧不一致的状态，那是在对审计撒谎。
func TestSweepApprovalTimeouts_OfflineTargetKeepsApprovalPending(t *testing.T) {
	svc, approvals, _ := timeoutFixture(t, 60, 10*time.Minute, gateway.ErrNoRunner)

	closed, err := svc.SweepApprovalTimeouts(context.Background(), time.Now())
	if err != nil {
		t.Fatalf("sweep must not fail the pass when the target is offline: %v", err)
	}
	if closed != 0 {
		t.Fatalf("nothing was delivered, so nothing may be recorded as closed")
	}
	if len(approvals.updated) != 0 {
		t.Fatalf("approval must stay Pending for the next sweep, got %+v", approvals.updated)
	}
}

// 未装配审批记录（broker-only 路径）时是一次安全空转，不 panic。
func TestSweepApprovalTimeouts_WithoutStoreIsNoop(t *testing.T) {
	svc := &PipelineRunService{}
	closed, err := svc.SweepApprovalTimeouts(context.Background(), time.Now())
	if err != nil || closed != 0 {
		t.Fatalf("want a no-op, got closed=%d err=%v", closed, err)
	}
}

func TestRunApprovalTimeouts_DisabledByZeroInterval(t *testing.T) {
	svc, approvals, _ := timeoutFixture(t, 60, time.Hour, nil)
	// 立即返回 = 不启动 ticker，也不扫第一轮。
	done := make(chan struct{})
	go func() { svc.RunApprovalTimeouts(context.Background(), 0); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatalf("a zero interval must return immediately")
	}
	if len(approvals.updated) != 0 {
		t.Fatalf("disabled loop must not sweep")
	}
}

// ---- 生产强审批 ------------------------------------------------------------

type stubProductionPolicy struct {
	prod []uuid.UUID
	err  error
	got  [][]uuid.UUID
}

func (s *stubProductionPolicy) ProductionTargets(ids []uuid.UUID) ([]uuid.UUID, error) {
	s.got = append(s.got, ids)
	if s.err != nil {
		return nil, s.err
	}
	return s.prod, nil
}

// approvalGateService 与 newFullTestService 同构，但定义里**带**一个人工审核阶段。
func approvalGateService(disp Dispatcher, store DispatchJobStore) *PipelineRunService {
	svc := newFullTestService(disp, store)
	svc.taskTemplateRepo = gateTemplateStore{}
	return svc
}

type gateTemplateStore struct{}

func (gateTemplateStore) ListByStageID(uuid.UUID) ([]pipelinemodels.PipelineTaskTemplate, error) {
	return []pipelinemodels.PipelineTaskTemplate{
		{Name: "build", Type: runnerapi.TaskTypeBuild, ScriptArgs: datatypes.JSON("[]"),
			Produces: datatypes.JSON("[]"), Consumes: datatypes.JSON("[]"), RetryPolicy: datatypes.JSON(`{"maxRetries":0}`)},
		{Name: "approve", Type: runnerapi.TaskTypeApproval, ScriptArgs: datatypes.JSON("[]"),
			Produces: datatypes.JSON("[]"), Consumes: datatypes.JSON("[]"), RetryPolicy: datatypes.JSON(`{"maxRetries":0}`)},
	}, nil
}

func TestProductionGuard_RefusesProductionTriggerWithoutApprovalGate(t *testing.T) {
	store := newFakeDispatchStore()
	disp := &fakeDispatcher{}
	svc := newFullTestService(disp, store)
	targetID := uuid.New()
	svc.SetProductionPolicy(&stubProductionPolicy{prod: []uuid.UUID{targetID}})

	runs, err := svc.Trigger(uuid.New(), &models.TriggerRequest{TargetID: &targetID})

	if err == nil {
		t.Fatalf("a production trigger without an approval gate must be refused")
	}
	ae, ok := err.(*common.APIError)
	if !ok {
		t.Fatalf("want *common.APIError, got %T (%v)", err, err)
	}
	if ae.Code != http.StatusConflict || ae.ErrorCode != "ERR.08409005" {
		t.Fatalf("want 409 / ERR.08409005, got %d / %s", ae.Code, ae.ErrorCode)
	}
	if len(ae.Reasons) != 1 {
		t.Fatalf("the refusal must carry a structured reason, got %v", ae.Reasons)
	}
	// 拒绝必须发生在**建 run 之前**：半途拒绝会留下一个不该存在的运行。
	if len(runs) != 0 {
		t.Fatalf("no run may be created when the policy refuses the trigger, got %d", len(runs))
	}
	if len(store.all()) != 0 || disp.count() != 0 {
		t.Fatalf("no dispatch job may be enqueued when the trigger is refused")
	}
}

func TestProductionGuard_RefusesFanoutBeforeCreatingAnyRun(t *testing.T) {
	store := newFakeDispatchStore()
	svc := newFullTestService(&fakeDispatcher{}, store)
	prod, other := uuid.New(), uuid.New()
	svc.SetProductionPolicy(&stubProductionPolicy{prod: []uuid.UUID{prod}})

	if _, err := svc.Trigger(uuid.New(), &models.TriggerRequest{TargetIDs: []uuid.UUID{other, prod}}); err == nil {
		t.Fatalf("one production target in a fan-out must refuse the whole trigger")
	}
	if len(store.all()) != 0 {
		t.Fatalf("a refused fan-out must not leave partial runs/jobs behind")
	}
}

func TestProductionGuard_AllowsProductionWithApprovalGate(t *testing.T) {
	store := newFakeDispatchStore()
	svc := approvalGateService(&fakeDispatcher{}, store)
	targetID := uuid.New()
	svc.SetProductionPolicy(&stubProductionPolicy{prod: []uuid.UUID{targetID}})

	runs, err := svc.Trigger(uuid.New(), &models.TriggerRequest{TargetID: &targetID})
	if err != nil {
		t.Fatalf("a production pipeline WITH an approval gate must be allowed: %v", err)
	}
	if len(runs) != 1 {
		t.Fatalf("want 1 run, got %d", len(runs))
	}
}

func TestProductionGuard_AllowsNonProductionWithoutGate(t *testing.T) {
	svc := newFullTestService(&fakeDispatcher{}, newFakeDispatchStore())
	targetID := uuid.New()
	policy := &stubProductionPolicy{prod: nil} // 该 target 不是生产环境
	svc.SetProductionPolicy(policy)

	if _, err := svc.Trigger(uuid.New(), &models.TriggerRequest{TargetID: &targetID}); err != nil {
		t.Fatalf("non-production triggers are unaffected: %v", err)
	}
	if len(policy.got) != 1 || len(policy.got[0]) != 1 || policy.got[0][0] != targetID {
		t.Fatalf("the policy must be asked about the resolved target, got %v", policy.got)
	}
}

// 策略查询失败 ⇒ 拒绝（fail-closed）。放行一个本该被审批卡住的生产发布，代价远大于
// 一次"稍后重试触发"。
func TestProductionGuard_FailsClosedWhenPolicyLookupFails(t *testing.T) {
	svc := newFullTestService(&fakeDispatcher{}, newFakeDispatchStore())
	targetID := uuid.New()
	svc.SetProductionPolicy(&stubProductionPolicy{err: errors.New("db down")})

	_, err := svc.Trigger(uuid.New(), &models.TriggerRequest{TargetID: &targetID})
	if err == nil {
		t.Fatalf("a policy lookup failure must refuse the trigger, not allow it")
	}
}

// 未装配策略 = 没有平台级生产规则，触发照旧（与鉴权关闭时管理面裸挂同一口径）。
func TestProductionGuard_NoPolicyWiredAllows(t *testing.T) {
	svc := newFullTestService(&fakeDispatcher{}, newFakeDispatchStore())
	targetID := uuid.New()
	if _, err := svc.Trigger(uuid.New(), &models.TriggerRequest{TargetID: &targetID}); err != nil {
		t.Fatalf("without a policy, triggers are unaffected: %v", err)
	}
}

func TestProductionGuardTargets(t *testing.T) {
	a, b := uuid.New(), uuid.New()
	if got := productionGuardTargets(a, nil); len(got) != 1 || got[0] != a {
		t.Fatalf("single-target trigger must yield exactly that target, got %v", got)
	}
	if got := productionGuardTargets(a, []uuid.UUID{b}); len(got) != 1 || got[0] != b {
		t.Fatalf("fan-out wins over the single field (TargetIDs overrides TargetID), got %v", got)
	}
	if got := productionGuardTargets(uuid.Nil, nil); got != nil {
		t.Fatalf("no target resolved = nothing to guard, got %v", got)
	}
}
