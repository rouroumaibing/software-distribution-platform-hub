package service

import (
	"net/http"
	"testing"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/rouroumaibing/software-distribution-platform-hub/internal/common"
	"github.com/rouroumaibing/software-distribution-platform-hub/internal/pipeline/models"
)

// 覆盖 backlog B-15 的父存在性校验（"关掉孤儿制造入口"）。全部用内存 fake，
// 不需要 Postgres —— 与 run 包 pipeline_run_*_test.go 同一套路。

// ---- fakes ----------------------------------------------------------------

type fakePipelineParent struct {
	live map[uuid.UUID]models.Pipeline
}

// GetByID 对已软删/不存在的 pipeline 都返回 gorm.ErrRecordNotFound，与 GORM 的
// 软删 scope 行为一致（fake 必须复制这一点，否则测不出真实语义）。
func (f *fakePipelineParent) GetByID(id uuid.UUID) (*models.Pipeline, error) {
	if p, ok := f.live[id]; ok {
		cp := p
		return &cp, nil
	}
	return nil, gorm.ErrRecordNotFound
}

type fakeStageStore struct {
	rows    map[uuid.UUID]models.PipelineStage
	created []models.PipelineStage
}

func newFakeStageStore() *fakeStageStore {
	return &fakeStageStore{rows: map[uuid.UUID]models.PipelineStage{}}
}

func (f *fakeStageStore) Create(s *models.PipelineStage) error {
	// GORM 的 `default:gen_random_uuid()` 会在插入后把主键回填到实体上；fake 必须
	// 复制这一点，否则 `Create` 之后再拿 s.ID（回滚时的重建流程就是这么用的）
	// 会一直是零值，而真实行为不是。
	if s.ID == uuid.Nil {
		s.ID = uuid.New()
	}
	f.created = append(f.created, *s)
	f.rows[s.ID] = *s
	return nil
}

func (f *fakeStageStore) GetByID(id uuid.UUID) (*models.PipelineStage, error) {
	s, ok := f.rows[id]
	if !ok {
		return nil, gorm.ErrRecordNotFound
	}
	cp := s
	return &cp, nil
}

func (f *fakeStageStore) Update(s *models.PipelineStage) error { f.rows[s.ID] = *s; return nil }

func (f *fakeStageStore) Delete(id uuid.UUID) error { delete(f.rows, id); return nil }

func (f *fakeStageStore) ListByPipelineID(pipelineID uuid.UUID) ([]models.PipelineStage, error) {
	var out []models.PipelineStage
	for _, s := range f.rows {
		if s.PipelineID == pipelineID {
			out = append(out, s)
		}
	}
	return out, nil
}

func newGuardFixture(livePipelines ...uuid.UUID) (*StageService, *fakeStageStore) {
	parent := &fakePipelineParent{live: map[uuid.UUID]models.Pipeline{}}
	for _, id := range livePipelines {
		parent.live[id] = models.Pipeline{}
	}
	store := newFakeStageStore()
	// templates 传 nil：本组用例只测父存在性校验，不涉及软删级联。
	return NewStageService(store, parent, nil), store
}

func assertNotFound(t *testing.T, err error, wantErrorCode, what string) {
	t.Helper()
	if err == nil {
		t.Fatalf("%s: want 404 error, got nil", what)
	}
	ae, ok := err.(*common.APIError)
	if !ok {
		t.Fatalf("%s: want *common.APIError, got %T (%v)", what, err, err)
	}
	if ae.Code != http.StatusNotFound {
		t.Fatalf("%s: want HTTP 404, got %d (%s)", what, ae.Code, ae.ErrorCode)
	}
	if wantErrorCode != "" && ae.ErrorCode != wantErrorCode {
		t.Fatalf("%s: want %s, got %s", what, wantErrorCode, ae.ErrorCode)
	}
}

// ---- (a) stage: Create / List 必须校验父存在 ---------------------------------

func TestStageCreate_RefusesDeletedPipeline(t *testing.T) {
	svc, store := newGuardFixture() // 无活 pipeline：等价于 pipeline 已软删或不存在
	dead := uuid.New()

	err := svc.Create(&models.PipelineStage{PipelineID: dead, Name: "构建", Sequence: 1})
	assertNotFound(t, err, "ERR.08404001", "Create under deleted pipeline")
	if len(store.created) != 0 {
		t.Fatalf("stage was written despite missing parent: %+v", store.created)
	}
}

func TestStageCreate_AllowsLivePipeline(t *testing.T) {
	live := uuid.New()
	svc, store := newGuardFixture(live)

	if err := svc.Create(&models.PipelineStage{PipelineID: live, Name: "构建", Sequence: 1}); err != nil {
		t.Fatalf("Create under live pipeline: unexpected error %v", err)
	}
	if len(store.created) != 1 {
		t.Fatalf("want 1 stage written, got %d", len(store.created))
	}
}

func TestStageList_RefusesDeletedPipeline(t *testing.T) {
	svc, store := newGuardFixture()
	dead := uuid.New()
	store.rows[uuid.New()] = models.PipelineStage{PipelineID: dead, Name: "遗留阶段"}

	items, err := svc.ListByPipeline(dead)
	assertNotFound(t, err, "ERR.08404001", "List stages of deleted pipeline")
	if items != nil {
		t.Fatalf("want nil items on 404, got %+v", items)
	}
}

func TestStageList_ReturnsStagesOfLivePipeline(t *testing.T) {
	live := uuid.New()
	svc, store := newGuardFixture(live)
	store.rows[uuid.New()] = models.PipelineStage{PipelineID: live, Name: "构建", Sequence: 1}

	items, err := svc.ListByPipeline(live)
	if err != nil {
		t.Fatalf("ListByPipeline: unexpected error %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("want 1 stage, got %d", len(items))
	}
}

// ---- (a) task template: 父链 task -> stage -> pipeline 全都要在 ----------------

func TestEnsureStageExists_MissingStage(t *testing.T) {
	svc, _ := newGuardFixture(uuid.New())
	assertNotFound(t, svc.EnsureStageExists(uuid.New()), "ERR.08404002", "missing stage")
}

func TestEnsureStageExists_StageUnderDeletedPipeline(t *testing.T) {
	live := uuid.New()
	svc, store := newGuardFixture(live)
	stageID := uuid.New()
	store.rows[stageID] = models.PipelineStage{PipelineID: live, Name: "构建", Sequence: 1}
	if err := svc.EnsureStageExists(stageID); err != nil {
		t.Fatalf("live chain: unexpected error %v", err)
	}

	// 父 pipeline 软删（从活集合里移除）后，同一个 stage 也必须拒绝。
	svc, store = newGuardFixture() // 空活集合
	store.rows[stageID] = models.PipelineStage{PipelineID: live, Name: "构建", Sequence: 1}
	assertNotFound(t, svc.EnsureStageExists(stageID), "ERR.08404001", "stage under deleted pipeline")
}

func TestTaskTemplateCreate_RefusesBeforeTouchingRepo(t *testing.T) {
	svc, _ := newGuardFixture()
	// repo 传 nil：校验必须先于任何持久化访问返回，否则这里会 panic 而不是断言失败。
	tpl := NewTaskTemplateService(nil, svc)

	err := tpl.Create(&models.PipelineTaskTemplate{StageID: uuid.New(), Name: "构建镜像"})
	assertNotFound(t, err, "ERR.08404002", "task template under missing stage")
}

// ---- 有意的非对称：清理通道不能被关死 ----------------------------------------

func TestStageUpdateAndDelete_StayOpenOnDeletedPipeline(t *testing.T) {
	svc, store := newGuardFixture() // 父 pipeline 已软删
	id := uuid.New()
	row := models.PipelineStage{PipelineID: uuid.New(), Name: "旧名", Sequence: 1}
	row.ID = id // ID 是内嵌 common.Base 的提升字段，不能出现在复合字面量里
	store.rows[id] = row

	if _, err := svc.Update(id, &models.PipelineStage{Name: "新名", Sequence: 2}); err != nil {
		t.Fatalf("Update should stay allowed for cleanup, got %v", err)
	}
	if err := svc.Delete(id); err != nil {
		t.Fatalf("Delete should stay allowed for cleanup, got %v", err)
	}
}
