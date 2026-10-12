package service

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"
	"gorm.io/gorm"

	"github.com/rouroumaibing/software-distribution-platform-hub/internal/common"
	"github.com/rouroumaibing/software-distribution-platform-hub/internal/pipeline/models"
	runmodels "github.com/rouroumaibing/software-distribution-platform-hub/internal/run/models"
)

// ui-12 §4.3 / N-21 / steelman S-3：触发器服务的行为契约 —— 类型校验（非法
// cron 保存即拒）、webhook token 生命周期（铸造/轮换/常量比较）、cron tick 的
// due 判定与失败重试、run.finished 状态过滤。

// ---- fakes -----------------------------------------------------------------

type fakeTriggerStore struct {
	rows    map[uuid.UUID]*models.PipelineTrigger
	stamped map[uuid.UUID]time.Time
	nextID  int
}

func newFakeTriggerStore() *fakeTriggerStore {
	return &fakeTriggerStore{rows: map[uuid.UUID]*models.PipelineTrigger{}, stamped: map[uuid.UUID]time.Time{}}
}

func (f *fakeTriggerStore) Create(t *models.PipelineTrigger) error {
	f.nextID++
	t.ID = uuid.New()
	cp := *t
	f.rows[t.ID] = &cp
	return nil
}

func (f *fakeTriggerStore) GetByID(id uuid.UUID) (*models.PipelineTrigger, error) {
	if t, ok := f.rows[id]; ok {
		cp := *t
		return &cp, nil
	}
	return nil, gorm.ErrRecordNotFound
}

func (f *fakeTriggerStore) Update(t *models.PipelineTrigger) error {
	cp := *t
	f.rows[t.ID] = &cp
	return nil
}

func (f *fakeTriggerStore) Delete(id uuid.UUID) error {
	delete(f.rows, id)
	return nil
}

func (f *fakeTriggerStore) FindByPipeline(uuid.UUID, common.Pagination) ([]models.PipelineTrigger, int64, error) {
	return nil, 0, nil
}

func (f *fakeTriggerStore) FindEnabledByType(t string) ([]models.PipelineTrigger, error) {
	var out []models.PipelineTrigger
	for _, r := range f.rows {
		if r.Type == t && r.Enabled {
			cp := *r
			out = append(out, cp)
		}
	}
	return out, nil
}

func (f *fakeTriggerStore) ClaimDueCron(*gorm.DB) ([]models.PipelineTrigger, error) {
	var out []models.PipelineTrigger
	for _, r := range f.rows {
		if r.Type == models.TriggerTypeCron && r.Enabled {
			cp := *r
			out = append(out, cp)
		}
	}
	return out, nil
}

func (f *fakeTriggerStore) UpdateLastTriggered(_ *gorm.DB, id uuid.UUID, at time.Time) error {
	f.stamped[id] = at
	return nil
}

type fakeRunStarter struct {
	calls []struct {
		pipelineID uuid.UUID
		req        *runmodels.TriggerRequest
	}
	err error
}

func (f *fakeRunStarter) Trigger(pipelineID uuid.UUID, req *runmodels.TriggerRequest) ([]*runmodels.PipelineRun, error) {
	if f.err != nil {
		return nil, f.err
	}
	f.calls = append(f.calls, struct {
		pipelineID uuid.UUID
		req        *runmodels.TriggerRequest
	}{pipelineID, req})
	return []*runmodels.PipelineRun{{ID: uuid.New()}}, nil
}

// ---- helpers ---------------------------------------------------------------

func newTestTriggerSvc(t *testing.T, store TriggerStore, starter RunStarter, now time.Time) *TriggerService {
	t.Helper()
	svc := NewTriggerService(store, nil, starter)
	svc.now = func() time.Time { return now }
	return svc
}

// ---- validation ------------------------------------------------------------

func TestTriggerCreate_RejectsBadCron(t *testing.T) {
	store := newFakeTriggerStore()
	svc := newTestTriggerSvc(t, store, &fakeRunStarter{}, time.Now())
	_, err := svc.Create(uuid.New(), &models.PipelineTrigger{
		Name: "bad", Type: models.TriggerTypeCron, Spec: datatypes.JSON(`{"expr":"61 * * * *"}`),
	}, "tester")
	if err == nil || !strings.Contains(err.Error(), "分") {
		t.Fatalf("非法 cron 应保存即拒（分越界），得到: %v", err)
	}
	_, err = svc.Create(uuid.New(), &models.PipelineTrigger{
		Name: "noexpr", Type: models.TriggerTypeCron,
	}, "tester")
	if err == nil {
		t.Fatalf("缺 spec.expr 应报错")
	}
}

func TestTriggerCreate_WebhookMintsToken(t *testing.T) {
	store := newFakeTriggerStore()
	svc := newTestTriggerSvc(t, store, &fakeRunStarter{}, time.Now())
	trig, err := svc.Create(uuid.New(), &models.PipelineTrigger{
		Name: "hook", Type: models.TriggerTypeWebhook,
	}, "tester")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(trig.Token, "wh_") || len(trig.Token) < 32 {
		t.Fatalf("webhook token 应为 wh_ 前缀的高熵串: %q", trig.Token)
	}
	if _, ok := store.rows[trig.ID]; !ok {
		t.Fatalf("Create 未落库")
	}
}

func TestTriggerRotate_OnlyWebhook(t *testing.T) {
	store := newFakeTriggerStore()
	svc := newTestTriggerSvc(t, store, &fakeRunStarter{}, time.Now())
	cron, _ := svc.Create(uuid.New(), &models.PipelineTrigger{
		Name: "c", Type: models.TriggerTypeCron, Spec: datatypes.JSON(`{"expr":"* * * * *"}`),
	}, "tester")
	if _, err := svc.Rotate(cron.ID); err == nil {
		t.Fatalf("cron 触发器不应可轮换")
	}
	hook, _ := svc.Create(uuid.New(), &models.PipelineTrigger{
		Name: "h", Type: models.TriggerTypeWebhook,
	}, "tester")
	t1, _ := svc.Rotate(hook.ID)
	t2, _ := svc.Rotate(hook.ID)
	if t1 == t2 {
		t.Fatalf("轮换应产生新 token")
	}
}

func TestTriggerAuthorizeWebhook(t *testing.T) {
	store := newFakeTriggerStore()
	svc := newTestTriggerSvc(t, store, &fakeRunStarter{}, time.Now())
	hook, _ := svc.Create(uuid.New(), &models.PipelineTrigger{
		Name: "h", Type: models.TriggerTypeWebhook,
	}, "tester")
	if _, ok := svc.AuthorizeWebhook(hook.ID, hook.Token); !ok {
		t.Fatalf("正确 token 应通过")
	}
	if _, ok := svc.AuthorizeWebhook(hook.ID, hook.Token+"x"); ok {
		t.Fatalf("错误 token 应拒绝")
	}
	if _, ok := svc.AuthorizeWebhook(hook.ID, ""); ok {
		t.Fatalf("空 token 应拒绝")
	}
}

// ---- cron tick -------------------------------------------------------------

func TestTick_FiresDueCronAndStamps(t *testing.T) {
	store := newFakeTriggerStore()
	starter := &fakeRunStarter{}
	now := time.Date(2026, 10, 11, 10, 30, 0, 0, time.UTC)
	svc := newTestTriggerSvc(t, store, starter, now)
	trig, err := svc.Create(uuid.New(), &models.PipelineTrigger{
		Name: "every-minute", Type: models.TriggerTypeCron, Enabled: true,
		Spec:            datatypes.JSON(`{"expr":"* * * * *"}`),
		LastTriggeredAt: ptrTime(now.Add(-2 * time.Minute)),
	}, "tester")
	if err != nil {
		t.Fatal(err)
	}
	svc.tick()
	if len(starter.calls) != 1 {
		t.Fatalf("due 触发器应开跑一次，得到 %d 次", len(starter.calls))
	}
	call := starter.calls[0]
	if call.req.TriggerSource != "cron" || call.req.TriggeredBy != "cron:every-minute" {
		t.Fatalf("TriggerSource/TriggeredBy 不符: %+v", call.req)
	}
	if call.pipelineID != trig.PipelineID {
		t.Fatalf("触发 pipeline 不符")
	}
	if _, ok := store.stamped[trig.ID]; !ok {
		t.Fatalf("触发后应写 last_triggered_at")
	}
}

func TestTick_SkipsNotDueAndStampsOnlyOnSuccess(t *testing.T) {
	store := newFakeTriggerStore()
	now := time.Date(2026, 10, 11, 10, 30, 0, 0, time.UTC)
	starter := &fakeRunStarter{err: errBoom}
	svc := newTestTriggerSvc(t, store, starter, now)
	trig, _ := svc.Create(uuid.New(), &models.PipelineTrigger{
		Name: "daily", Type: models.TriggerTypeCron, Enabled: true,
		Spec:            datatypes.JSON(`{"expr":"0 9 * * *"}`),
		LastTriggeredAt: ptrTime(now.Add(-time.Hour)),
	}, "tester")
	svc.tick() // 9:00 已过但 fire 失败
	if len(starter.calls) != 0 {
		t.Fatalf("starter 失败不应计入成功调用")
	}
	if _, ok := store.stamped[trig.ID]; ok {
		t.Fatalf("fire 失败不应写 last_triggered_at（下一 tick 重试）")
	}
}

func TestTick_BadSpecSkipped(t *testing.T) {
	store := newFakeTriggerStore()
	now := time.Now()
	svc := newTestTriggerSvc(t, store, &fakeRunStarter{}, now)
	_, _ = svc.Create(uuid.New(), &models.PipelineTrigger{
		Name: "broken", Type: models.TriggerTypeCron, Enabled: true, Spec: datatypes.JSON(`{}`),
	}, "tester")
	svc.tick() // 不 panic、不触发
}

// ---- run.finished event -----------------------------------------------------

func TestEvaluateRunFinished_MatchesStatusFilter(t *testing.T) {
	store := newFakeTriggerStore()
	starter := &fakeRunStarter{}
	now := time.Now()
	svc := newTestTriggerSvc(t, store, starter, now)
	upstream := uuid.New()
	spec, _ := models.EncodeEventSpec(models.EventSpec{OnPipelineID: upstream, Statuses: []string{"Succeeded"}})
	_, _ = svc.Create(uuid.New(), &models.PipelineTrigger{
		Name: "chain", Type: models.TriggerTypeEvent, Enabled: true, Spec: spec,
	}, "tester")

	svc.EvaluateRunFinished(upstream, "Failed")
	if len(starter.calls) != 0 {
		t.Fatalf("状态不匹配不应触发")
	}
	svc.EvaluateRunFinished(upstream, "Succeeded")
	if len(starter.calls) != 1 {
		t.Fatalf("状态匹配应触发一次，得到 %d", len(starter.calls))
	}
	if starter.calls[0].req.TriggerSource != "event" {
		t.Fatalf("TriggerSource 应为 event")
	}
}

func TestEvaluateRunFinished_DefaultStatusesSucceeded(t *testing.T) {
	store := newFakeTriggerStore()
	starter := &fakeRunStarter{}
	now := time.Now()
	svc := newTestTriggerSvc(t, store, starter, now)
	upstream := uuid.New()
	spec, _ := models.EncodeEventSpec(models.EventSpec{OnPipelineID: upstream})
	_, _ = svc.Create(uuid.New(), &models.PipelineTrigger{
		Name: "chain", Type: models.TriggerTypeEvent, Enabled: true, Spec: spec,
	}, "tester")
	svc.EvaluateRunFinished(upstream, "Cancelled")
	if len(starter.calls) != 0 {
		t.Fatalf("缺省 statuses=Succeeded，Cancelled 不应触发")
	}
	svc.EvaluateRunFinished(upstream, "Succeeded")
	if len(starter.calls) != 1 {
		t.Fatalf("Succeeded 应触发")
	}
}

// ---- misc -------------------------------------------------------------------

type boomErr struct{}

func (boomErr) Error() string { return "boom" }

var errBoom = boomErr{}

func ptrTime(t time.Time) *time.Time { return &t }
