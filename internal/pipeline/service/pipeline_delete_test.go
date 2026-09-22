package service

import (
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/rouroumaibing/software-distribution-platform-hub/internal/common"
	"github.com/rouroumaibing/software-distribution-platform-hub/internal/pipeline/models"
)

// 覆盖 backlog B-12 / B-15（pipeline 包）：
//   - PipelineService.Delete 从"任意历史即拒"收窄为"仅活跃 phase 拒绝"；
//   - StageService.Delete 的软删级联（模板先删、stage 后删，失败可重试）。

// ---- fakes ----------------------------------------------------------------

type fakePipelineStore struct{ deleted []uuid.UUID }

func (f *fakePipelineStore) Create(*models.Pipeline) error { return nil }

func (f *fakePipelineStore) GetByID(uuid.UUID) (*models.Pipeline, error) { return nil, nil }

func (f *fakePipelineStore) Update(*models.Pipeline) error { return nil }

func (f *fakePipelineStore) Delete(id uuid.UUID) error {
	f.deleted = append(f.deleted, id)
	return nil
}

func (f *fakePipelineStore) FindByComponentID(uuid.UUID, common.Pagination) ([]models.Pipeline, int64, error) {
	return nil, 0, nil
}

func (f *fakePipelineStore) FindAll(uuid.UUID, string, string, common.Pagination) ([]models.Pipeline, int64, error) {
	return nil, 0, nil
}

// fakeVersionPublisher 代替早前的 fakeVersionStore：PipelineService 现在只接受
// 一个**发布者**（VersionPublisher），自己不再能拼出一个半截版本行（C-09）。
type fakeVersionPublisher struct {
	published []uuid.UUID
	err       error
}

func (f *fakeVersionPublisher) Publish(pipelineID uuid.UUID, _ string) error {
	f.published = append(f.published, pipelineID)
	return f.err
}

// fakeRunExistence 记下 CountByPipeline（旧语义用的"任意历史"）是否被调用：
// 收窄后的实现**不应**再碰它。
type fakeRunExistence struct {
	history      int64
	active       int64
	phases       []string
	historyCalls int
}

func (f *fakeRunExistence) CountByPipeline(uuid.UUID) (int64, error) {
	f.historyCalls++
	return f.history, nil
}

func (f *fakeRunExistence) CountActiveByPipeline(_ uuid.UUID, phases []string) (int64, error) {
	f.phases = append([]string(nil), phases...)
	return f.active, nil
}

type fakeTemplateCascade struct {
	got []uuid.UUID
	err error
}

func (f *fakeTemplateCascade) DeleteByStageID(stageID uuid.UUID) error {
	f.got = append(f.got, stageID)
	return f.err
}

// ---- pipeline delete ------------------------------------------------------

func TestPipelineDelete_RejectsActiveRuns(t *testing.T) {
	store := &fakePipelineStore{}
	run := &fakeRunExistence{history: 42, active: 1}
	svc := NewPipelineService(store, &fakeVersionPublisher{}, run)

	err := svc.Delete(uuid.New())

	if err == nil {
		t.Fatalf("want HTTP 409, got nil error")
	}
	ae, ok := err.(*common.APIError)
	if !ok {
		t.Fatalf("want *common.APIError, got %T (%v)", err, err)
	}
	if ae.Code != http.StatusConflict {
		t.Fatalf("want HTTP 409, got %d (%s)", ae.Code, ae.ErrorCode)
	}
	if ae.ErrorCode != "ERR.08409001" {
		t.Fatalf("want ERR.08409001, got %s", ae.ErrorCode)
	}
	if len(ae.Reasons) != 1 || !strings.Contains(ae.Reasons[0], "1 条运行") {
		t.Fatalf("reason should carry the active count, got %v", ae.Reasons)
	}
	if len(store.deleted) != 0 {
		t.Fatalf("repo must not be touched when the delete is rejected: %v", store.deleted)
	}
	if len(run.phases) != 3 {
		t.Fatalf("must count against exactly the active phases, got %v", run.phases)
	}
}

// 关键回归：42 条历史运行 + 0 条活跃 → 必须放行。旧实现会因为这 42 条历史拒删，
// 让废弃流水线永远删不掉（DELETE-CONTRACT §2.1 / §6.4 #9）。
func TestPipelineDelete_AllowsWhenOnlyHistoricalRuns(t *testing.T) {
	store := &fakePipelineStore{}
	run := &fakeRunExistence{history: 42, active: 0}
	svc := NewPipelineService(store, &fakeVersionPublisher{}, run)
	id := uuid.New()

	if err := svc.Delete(id); err != nil {
		t.Fatalf("historical-only runs must not block deletion: %v", err)
	}
	if len(store.deleted) != 1 || store.deleted[0] != id {
		t.Fatalf("want repo delete for %s, got %v", id, store.deleted)
	}
	if run.historyCalls != 0 {
		t.Fatalf("Delete must no longer consult raw run history (old 'any history rejects' semantics)")
	}
}

// ---- stage soft-delete cascade -------------------------------------------

func TestStageDelete_CascadesToTemplatesThenDeletesStage(t *testing.T) {
	store := newFakeStageStore()
	stageID := uuid.New()
	store.rows[stageID] = models.PipelineStage{PipelineID: uuid.New(), Name: "构建", Sequence: 1}
	cascade := &fakeTemplateCascade{}
	svc := NewStageService(store, &fakePipelineParent{live: map[uuid.UUID]models.Pipeline{}}, cascade)

	if err := svc.Delete(stageID); err != nil {
		t.Fatalf("Delete: unexpected error %v", err)
	}
	if len(cascade.got) != 1 || cascade.got[0] != stageID {
		t.Fatalf("templates must be cascade-soft-deleted for stage %s, got %v", stageID, cascade.got)
	}
	if _, still := store.rows[stageID]; still {
		t.Fatalf("stage row should be gone after Delete")
	}
}

// 模板先删、stage 后删的顺序保证：级联失败时 stage 仍在，整个删除可重试，
// 不会留下"stage 已删、模板还是活行"的不一致状态。
func TestStageDelete_StopsWhenCascadeFails(t *testing.T) {
	store := newFakeStageStore()
	stageID := uuid.New()
	store.rows[stageID] = models.PipelineStage{PipelineID: uuid.New(), Name: "构建", Sequence: 1}
	svc := NewStageService(store, &fakePipelineParent{live: map[uuid.UUID]models.Pipeline{}},
		&fakeTemplateCascade{err: errors.New("boom")})

	if err := svc.Delete(stageID); err == nil {
		t.Fatalf("want the cascade error to surface")
	}
	if _, still := store.rows[stageID]; !still {
		t.Fatalf("stage must stay alive so the delete is retriable")
	}
}

// 未装配级联（单测/精简装配路径）时，Delete 仍需可用 —— 否则清理通道会被关死。
func TestStageDelete_WithoutCascadeStillDeletes(t *testing.T) {
	store := newFakeStageStore()
	stageID := uuid.New()
	store.rows[stageID] = models.PipelineStage{PipelineID: uuid.New(), Name: "构建", Sequence: 1}
	svc := NewStageService(store, &fakePipelineParent{live: map[uuid.UUID]models.Pipeline{}}, nil)

	if err := svc.Delete(stageID); err != nil {
		t.Fatalf("Delete without cascade wiring must not fail: %v", err)
	}
	if _, still := store.rows[stageID]; still {
		t.Fatalf("stage should be deleted")
	}
}
