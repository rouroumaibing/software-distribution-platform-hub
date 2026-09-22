package service

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"
	"gorm.io/gorm"

	runnerapi "github.com/rouroumaibing/software-distribution-platform-runner/api/v1alpha1"

	"github.com/rouroumaibing/software-distribution-platform-hub/internal/common"
	"github.com/rouroumaibing/software-distribution-platform-hub/internal/pipeline/models"
)

// 覆盖 backlog C-09（流水线版本历史 / 对比 / 回滚），全部内存 fake、不需要 Postgres。
//
// 这里钉住的是**语义**而不是实现：
//   - 结构改动必须留档，且"体没变"不产生新版本（否则一次 [保存] 会造出十几版空历史）；
//   - 版本号只有一个自增点（Publish），不会出现"号码跳了但没有快照"；
//   - 回滚只重建结构、历史只追加（旧版本行与版本号都不重写）。

// ---- fakes ----------------------------------------------------------------

type fakeVersionPipelineStore struct {
	rows    map[uuid.UUID]*models.Pipeline
	updates int
}

func newFakeVersionPipelineStore(p *models.Pipeline) *fakeVersionPipelineStore {
	return &fakeVersionPipelineStore{rows: map[uuid.UUID]*models.Pipeline{p.ID: p}}
}

func (f *fakeVersionPipelineStore) GetByID(id uuid.UUID) (*models.Pipeline, error) {
	p, ok := f.rows[id]
	if !ok {
		return nil, gorm.ErrRecordNotFound
	}
	cp := *p
	return &cp, nil
}

func (f *fakeVersionPipelineStore) Update(p *models.Pipeline) error {
	cp := *p
	f.rows[p.ID] = &cp
	f.updates++
	return nil
}

type fakeTaskStore struct {
	rows    map[uuid.UUID][]models.PipelineTaskTemplate
	created []models.PipelineTaskTemplate
}

func newFakeTaskStore() *fakeTaskStore {
	return &fakeTaskStore{rows: map[uuid.UUID][]models.PipelineTaskTemplate{}}
}

func (f *fakeTaskStore) Create(t *models.PipelineTaskTemplate) error {
	if t.ID == uuid.Nil {
		t.ID = uuid.New()
	}
	f.created = append(f.created, *t)
	f.rows[t.StageID] = append(f.rows[t.StageID], *t)
	return nil
}

func (f *fakeTaskStore) ListByStageID(stageID uuid.UUID) ([]models.PipelineTaskTemplate, error) {
	return f.rows[stageID], nil
}

func (f *fakeTaskStore) DeleteByStageID(stageID uuid.UUID) error {
	delete(f.rows, stageID)
	return nil
}

type fakeVersionRepo struct {
	rows []models.PipelineVersion
}

func (f *fakeVersionRepo) Create(v *models.PipelineVersion) error {
	if v.ID == uuid.Nil {
		v.ID = uuid.New()
	}
	if v.CreatedAt.IsZero() {
		v.CreatedAt = time.Unix(0, 0)
	}
	f.rows = append(f.rows, *v)
	return nil
}

func (f *fakeVersionRepo) ListByPipelineID(pipelineID uuid.UUID, limit int) ([]models.PipelineVersion, error) {
	var out []models.PipelineVersion
	for i := len(f.rows) - 1; i >= 0; i-- { // newest first
		if f.rows[i].PipelineID != pipelineID {
			continue
		}
		out = append(out, f.rows[i])
		if limit > 0 && len(out) == limit {
			break
		}
	}
	return out, nil
}

func (f *fakeVersionRepo) GetByVersion(pipelineID uuid.UUID, version int) (*models.PipelineVersion, error) {
	for i := range f.rows {
		if f.rows[i].PipelineID == pipelineID && f.rows[i].Version == version {
			cp := f.rows[i]
			return &cp, nil
		}
	}
	return nil, gorm.ErrRecordNotFound
}

func (f *fakeVersionRepo) Latest(pipelineID uuid.UUID) (*models.PipelineVersion, error) {
	var best *models.PipelineVersion
	for i := range f.rows {
		if f.rows[i].PipelineID != pipelineID {
			continue
		}
		if best == nil || f.rows[i].Version > best.Version {
			cp := f.rows[i]
			best = &cp
		}
	}
	if best == nil {
		return nil, gorm.ErrRecordNotFound
	}
	return best, nil
}

// fixture wires a pipeline with one stage + one task into all four fakes.
type versionFixture struct {
	svc       *PipelineVersionService
	pipelines *fakeVersionPipelineStore
	stages    *fakeStageStore
	tasks     *fakeTaskStore
	versions  *fakeVersionRepo
	pipeline  models.Pipeline
	stage     models.PipelineStage
}

func newVersionFixture(t *testing.T) *versionFixture {
	t.Helper()
	p := models.Pipeline{
		ComponentID: uuid.New(),
		Name:        "日常流水线",
		Kind:        "build",
		Version:     1,
	}
	p.ID = uuid.New()

	st := models.PipelineStage{PipelineID: p.ID, Name: "构建", Sequence: 1, ExecutionMode: models.ExecutionModeParallel}
	st.ID = uuid.New()

	stages := newFakeStageStore()
	stages.rows[st.ID] = st

	tasks := newFakeTaskStore()
	tasks.rows[st.ID] = []models.PipelineTaskTemplate{{
		StageID:      st.ID,
		Name:         "compile",
		Type:         runnerapi.TaskTypeBuild,
		DisplayOrder: 1,
		Image:        "golang:1.22",
		Command:      datatypes.JSON(`["go","build","./..."]`),
		ScriptArgs:   datatypes.JSON(`[]`),
		Args:         datatypes.JSON(`[]`),
		Produces:     datatypes.JSON(`[]`),
		Consumes:     datatypes.JSON(`[]`),
		RetryPolicy:  datatypes.JSON(`{"maxRetries":0}`),
	}}

	pipelines := newFakeVersionPipelineStore(&p)
	versions := &fakeVersionRepo{}
	return &versionFixture{
		svc:       NewPipelineVersionService(pipelines, stages, tasks, versions),
		pipelines: pipelines,
		stages:    stages,
		tasks:     tasks,
		versions:  versions,
		pipeline:  p,
		stage:     st,
	}
}

// ---- Publish：留档 + 去重 + 单一自增点 -------------------------------------

func TestPublish_RecordsFirstSnapshotAndBumpsVersion(t *testing.T) {
	f := newVersionFixture(t)

	if err := f.svc.Publish(f.pipeline.ID, "alice"); err != nil {
		t.Fatalf("Publish: %v", err)
	}
	if len(f.versions.rows) != 1 {
		t.Fatalf("want 1 version row, got %d", len(f.versions.rows))
	}
	got := f.versions.rows[0]
	if got.Version != 2 {
		t.Fatalf("version should be 2 (was 1 at create), got %d", got.Version)
	}
	if f.pipelines.rows[f.pipeline.ID].Version != 2 {
		t.Fatalf("pipelines.version should have been bumped to 2")
	}
	if got.CreatedBy != "alice" {
		t.Fatalf("createdBy not recorded: %q", got.CreatedBy)
	}
	snap, err := models.DecodeSnapshot(got.Snapshot)
	if err != nil {
		t.Fatalf("snapshot must decode: %v", err)
	}
	if snap.Name != "日常流水线" || len(snap.Stages) != 1 || len(snap.Stages[0].Tasks) != 1 {
		t.Fatalf("snapshot body incomplete: %+v", snap)
	}
}

// 关键：console 的 [保存] 会对每个阶段 / 子任务各发一次 PUT，绝大多数并未改动。
// 若每次 PUT 都记一版，一次保存会凭空造出十几版历史，把真正有内容的那版埋掉。
func TestPublish_SkipsWhenBodyUnchanged(t *testing.T) {
	f := newVersionFixture(t)
	if err := f.svc.Publish(f.pipeline.ID, "alice"); err != nil {
		t.Fatalf("first Publish: %v", err)
	}

	if err := f.svc.Publish(f.pipeline.ID, "alice"); err != nil {
		t.Fatalf("second Publish: %v", err)
	}
	if len(f.versions.rows) != 1 {
		t.Fatalf("identical body must not create a new version, got %d rows", len(f.versions.rows))
	}
	if f.pipelines.rows[f.pipeline.ID].Version != 2 {
		t.Fatalf("version must not be bumped for a no-op publish, got %d", f.pipelines.rows[f.pipeline.ID].Version)
	}
}

func TestPublish_RecordsNewVersionAfterStructuralChange(t *testing.T) {
	f := newVersionFixture(t)
	if err := f.svc.Publish(f.pipeline.ID, "alice"); err != nil {
		t.Fatalf("first Publish: %v", err)
	}

	// 新增一个阶段 → 结构变了 → 必须留档。
	newStage := models.PipelineStage{PipelineID: f.pipeline.ID, Name: "测试", Sequence: 2, ExecutionMode: models.ExecutionModeSerial}
	newStage.ID = uuid.New()
	f.stages.rows[newStage.ID] = newStage

	if err := f.svc.Publish(f.pipeline.ID, "alice"); err != nil {
		t.Fatalf("second Publish: %v", err)
	}
	if len(f.versions.rows) != 2 {
		t.Fatalf("structural change must create a version, got %d rows", len(f.versions.rows))
	}
	if f.pipelines.rows[f.pipeline.ID].Version != 3 {
		t.Fatalf("version should be 3, got %d", f.pipelines.rows[f.pipeline.ID].Version)
	}
}

func TestPublish_FailsOnUnknownPipeline(t *testing.T) {
	f := newVersionFixture(t)
	if err := f.svc.Publish(uuid.New(), "alice"); err == nil {
		t.Fatalf("want an error for an unknown pipeline")
	}
}

// ---- List / Get ----------------------------------------------------------

func TestList_CarriesCountsAndChangeMarker(t *testing.T) {
	f := newVersionFixture(t)
	_ = f.svc.Publish(f.pipeline.ID, "alice") // v2
	// 改一条子任务的命令 → v3
	tpl := f.tasks.rows[f.stage.ID][0]
	tpl.Command = datatypes.JSON(`["go","test","./..."]`)
	f.tasks.rows[f.stage.ID] = []models.PipelineTaskTemplate{tpl}
	_ = f.svc.Publish(f.pipeline.ID, "bob") // v3

	items, err := f.svc.List(f.pipeline.ID, 0)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("want 2 versions, got %d", len(items))
	}
	if !items[0].IsCurrent || items[0].Version != 3 {
		t.Fatalf("first item should be the current (v3), got %+v", items[0])
	}
	if items[0].Stages != 1 || items[0].Tasks != 1 {
		t.Fatalf("counts wrong: stages=%d tasks=%d", items[0].Stages, items[0].Tasks)
	}
	if items[0].IdenticalToPrevious == nil || *items[0].IdenticalToPrevious {
		t.Fatalf("v3 differs from v2, want identicalToPrevious=false")
	}
	// v2 是窗口内最老的：它的上一版（v1，创建时未记）不在窗口里 ⇒ 必须为"未知"，
	// 不能默认成 false 骗用户"有变化"。
	if items[1].IdenticalToPrevious != nil {
		t.Fatalf("oldest in window should report unknown, got %v", *items[1].IdenticalToPrevious)
	}
}

func TestList_RejectsOverMaxLimit(t *testing.T) {
	f := newVersionFixture(t)
	_, err := f.svc.List(f.pipeline.ID, MaxVersionListLimit+1)
	ae, ok := err.(*common.APIError)
	if !ok {
		t.Fatalf("want *common.APIError, got %T (%v)", err, err)
	}
	if ae.Code != http.StatusBadRequest || ae.ErrorCode != "ERR.08400004" {
		t.Fatalf("want 400 / ERR.08400004, got %d / %s", ae.Code, ae.ErrorCode)
	}
}

func TestGet_UnknownVersionIs404(t *testing.T) {
	f := newVersionFixture(t)
	_, err := f.svc.Get(f.pipeline.ID, 99)
	ae, ok := err.(*common.APIError)
	if !ok {
		t.Fatalf("want *common.APIError, got %T (%v)", err, err)
	}
	if ae.Code != http.StatusNotFound || ae.ErrorCode != "ERR.08404003" {
		t.Fatalf("want 404 / ERR.08404003, got %d / %s", ae.Code, ae.ErrorCode)
	}

	if _, err := f.svc.Get(f.pipeline.ID, 0); err == nil {
		t.Fatalf("version 0 must be rejected as a bad request")
	}
}

// ---- Compare -------------------------------------------------------------

func TestCompare_IsDirectionalAndNamed(t *testing.T) {
	f := newVersionFixture(t)
	_ = f.svc.Publish(f.pipeline.ID, "alice") // v2: 构建/compile

	// v3：把子任务改名（等于删一个 + 加一个）
	f.tasks.rows[f.stage.ID] = []models.PipelineTaskTemplate{{
		StageID: f.stage.ID, Name: "build-image", Type: runnerapi.TaskTypeBuild, DisplayOrder: 1,
		ScriptArgs: datatypes.JSON(`[]`), Command: datatypes.JSON(`[]`), Args: datatypes.JSON(`[]`),
		Produces: datatypes.JSON(`[]`), Consumes: datatypes.JSON(`[]`), RetryPolicy: datatypes.JSON(`{"maxRetries":0}`),
	}}
	_ = f.svc.Publish(f.pipeline.ID, "bob") // v3

	forward, err := f.svc.Compare(f.pipeline.ID, 2, 3)
	if err != nil {
		t.Fatalf("Compare: %v", err)
	}
	if forward.FromVersion != 2 || forward.ToVersion != 3 {
		t.Fatalf("diff must carry both version numbers, got %d → %d", forward.FromVersion, forward.ToVersion)
	}
	if forward.Identical {
		t.Fatalf("v2 and v3 differ")
	}
	if forward.Summary.TasksAdded != 1 || forward.Summary.TasksRemoved != 1 {
		t.Fatalf("want 1 added + 1 removed task, got %+v", forward.Summary)
	}

	// 反向：added / removed 互换（diff 是有方向的，不是对称集合比较）。
	backward, err := f.svc.Compare(f.pipeline.ID, 3, 2)
	if err != nil {
		t.Fatalf("Compare reversed: %v", err)
	}
	var names []string
	for _, td := range backward.Tasks {
		names = append(names, td.Name+"="+string(td.Change))
	}
	if backward.Summary.TasksAdded != 1 || backward.Summary.TasksRemoved != 1 {
		t.Fatalf("reverse diff should still report 1/1, got %+v", backward.Summary)
	}
	if len(names) != 2 {
		t.Fatalf("want 2 task diffs, got %v", names)
	}
}

// ---- Rollback ------------------------------------------------------------

func TestRollback_RematerializesAndAppendsNewVersion(t *testing.T) {
	f := newVersionFixture(t)
	_ = f.svc.Publish(f.pipeline.ID, "alice") // v2: 构建/compile

	// v3：删掉原阶段，换成 测试/pytest
	old := f.stages.rows[f.stage.ID]
	_ = f.tasks.DeleteByStageID(old.ID)
	_ = f.stages.Delete(old.ID)
	newStage := models.PipelineStage{PipelineID: f.pipeline.ID, Name: "测试", Sequence: 1, ExecutionMode: models.ExecutionModeParallel}
	newStage.ID = uuid.New()
	f.stages.rows[newStage.ID] = newStage
	f.tasks.rows[newStage.ID] = []models.PipelineTaskTemplate{{
		StageID: newStage.ID, Name: "pytest", Type: runnerapi.TaskTypeBuild, DisplayOrder: 1,
		ScriptArgs: datatypes.JSON(`[]`), Command: datatypes.JSON(`["pytest"]`), Args: datatypes.JSON(`[]`),
		Produces: datatypes.JSON(`[]`), Consumes: datatypes.JSON(`[]`), RetryPolicy: datatypes.JSON(`{"maxRetries":0}`),
	}}
	_ = f.svc.Publish(f.pipeline.ID, "bob") // v3

	res, err := f.svc.Rollback(f.pipeline.ID, 2, "carol")
	if err != nil {
		t.Fatalf("Rollback: %v", err)
	}
	if res.RestoredVersion != 2 || res.Stages != 1 || res.Tasks != 1 {
		t.Fatalf("unexpected rollback result %+v", res)
	}
	// 历史只追加：v2/v3 都还在，回滚自身记为 v4。
	if len(f.versions.rows) != 3 {
		t.Fatalf("history must be append-only (3 rows: v2/v3/v4), got %d", len(f.versions.rows))
	}
	if res.NewVersion != 4 || f.pipelines.rows[f.pipeline.ID].Version != 4 {
		t.Fatalf("rollback should be recorded as v4, got result=%d pipeline=%d",
			res.NewVersion, f.pipelines.rows[f.pipeline.ID].Version)
	}

	// 结构必须真的回到 v2：只剩一个"构建/compile"阶段。
	stages, _ := f.stages.ListByPipelineID(f.pipeline.ID)
	if len(stages) != 1 || stages[0].Name != "构建" {
		t.Fatalf("structure not restored: %+v", stages)
	}
	tasks := f.tasks.rows[stages[0].ID]
	if len(tasks) != 1 || tasks[0].Name != "compile" {
		t.Fatalf("tasks not restored: %+v", tasks)
	}

	// 回滚后的定义体应与 v2 的结构一致 —— 用 diff 反证，而不是靠肉眼看字段。
	after, err := f.svc.Compare(f.pipeline.ID, 2, res.NewVersion)
	if err != nil {
		t.Fatalf("Compare after rollback: %v", err)
	}
	if !after.Identical {
		t.Fatalf("rolled-back structure must equal the restored version, diff=%+v", after)
	}
}

func TestRollback_UnknownVersionIs404AndTouchesNothing(t *testing.T) {
	f := newVersionFixture(t)
	before := len(f.stages.rows)

	_, err := f.svc.Rollback(f.pipeline.ID, 42, "carol")
	ae, ok := err.(*common.APIError)
	if !ok {
		t.Fatalf("want *common.APIError, got %T (%v)", err, err)
	}
	if ae.Code != http.StatusNotFound {
		t.Fatalf("want 404, got %d", ae.Code)
	}
	if len(f.stages.rows) != before {
		t.Fatalf("a failed rollback must not clear the live structure")
	}
}

// 回滚重建时，未配置的可空 JSON 列必须保持为空 —— 补一个 '{}' 会把"未配置"
// 伪装成"已配置"（Release/Approval 任务的派生类型判断就会因此错编）。
func TestRollback_KeepsUnsetOptionalConfigsNil(t *testing.T) {
	f := newVersionFixture(t)
	if err := f.svc.Publish(f.pipeline.ID, "alice"); err != nil {
		t.Fatalf("Publish: %v", err)
	}
	if _, err := f.svc.Rollback(f.pipeline.ID, 2, "carol"); err != nil {
		t.Fatalf("Rollback: %v", err)
	}
	created := f.tasks.created[len(f.tasks.created)-1]
	if len(created.ReleaseConfig) != 0 || len(created.ApprovalConfig) != 0 || len(created.RolloutConfig) != 0 {
		t.Fatalf("unset optional configs must stay empty, got release=%s approval=%s rollout=%s",
			created.ReleaseConfig, created.ApprovalConfig, created.RolloutConfig)
	}
	// NOT NULL 列则必须回填默认值，否则重建会撞 23502。
	if string(created.ScriptArgs) != "[]" || string(created.RetryPolicy) != `{"maxRetries":0}` {
		t.Fatalf("NOT NULL json columns must be backfilled, got scriptArgs=%s retryPolicy=%s",
			created.ScriptArgs, created.RetryPolicy)
	}
}

func TestBuildSnapshot_PreservesRawJSONBytes(t *testing.T) {
	p := models.Pipeline{Name: "p", Kind: "build"}
	p.ID = uuid.New()
	st := models.PipelineStage{PipelineID: p.ID, Name: "构建", Sequence: 1}
	st.ID = uuid.New()

	// 键序故意反常：快照必须原样保留，不能被重新序列化成另一种写法。
	raw := `{"maxRetries":2,"backoff":"30s"}`
	snap := models.BuildSnapshot(&p, []models.PipelineStage{st}, map[uuid.UUID][]models.PipelineTaskTemplate{
		st.ID: {{Name: "t", Type: runnerapi.TaskTypeBuild, RetryPolicy: datatypes.JSON(raw)}},
	})
	if got := string(snap.Stages[0].Tasks[0].RetryPolicy); got != raw {
		t.Fatalf("raw JSON must be preserved verbatim, got %s", got)
	}
	body, err := json.Marshal(snap)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	// 无任务的阶段序列化成 []，不是 null —— 否则"结构相同"的两份快照会因
	// null / [] 之差被判成不同，凭空多出版本。
	if !json.Valid(body) {
		t.Fatalf("snapshot must be valid JSON")
	}
}
