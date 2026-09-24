package service

// C-06 serial executionMode 调度行为的单测：buildSpec 须为 serial 阶段内的
// 任务派生「同阶段紧邻前驱」依赖链；parallel（默认）保持无阶段内依赖。

import (
	"testing"

	"github.com/google/uuid"
	"gorm.io/datatypes"

	runnerapi "github.com/rouroumaibing/software-distribution-platform-runner/api/v1alpha1"

	pipelinemodels "github.com/rouroumaibing/software-distribution-platform-hub/internal/pipeline/models"
	"github.com/rouroumaibing/software-distribution-platform-hub/internal/run/models"
)

// --- minimal fakes (namespaced to avoid clashes with dispatch-test fakes) ---

type serialPipelineRepo struct{ p *pipelinemodels.Pipeline }

func (f *serialPipelineRepo) GetByID(uuid.UUID) (*pipelinemodels.Pipeline, error) { return f.p, nil }

type serialStageRepo struct {
	stages []pipelinemodels.PipelineStage
}

func (f *serialStageRepo) ListByPipelineID(uuid.UUID) ([]pipelinemodels.PipelineStage, error) {
	return f.stages, nil
}

type serialTplRepo struct {
	byStage map[uuid.UUID][]pipelinemodels.PipelineTaskTemplate
}

func (f *serialTplRepo) ListByStageID(id uuid.UUID) ([]pipelinemodels.PipelineTaskTemplate, error) {
	return f.byStage[id], nil
}

func serialTask(name string) pipelinemodels.PipelineTaskTemplate {
	return pipelinemodels.PipelineTaskTemplate{
		Name:       name,
		Type:       runnerapi.TaskTypeBuild,
		ScriptArgs: datatypes.JSON("[]"),
		Produces:   datatypes.JSON("null"),
		Consumes:   datatypes.JSON("null"),
	}
}

func newSerialTestSvc(t *testing.T, stages []pipelinemodels.PipelineStage, tplByStage map[uuid.UUID][]pipelinemodels.PipelineTaskTemplate) *PipelineRunService {
	t.Helper()
	pid := uuid.New()
	return &PipelineRunService{
		pipelineRepo:     &serialPipelineRepo{p: &pipelinemodels.Pipeline{ID: pid, Name: "p", ComponentID: uuid.New()}},
		stageRepo:        &serialStageRepo{stages: stages},
		taskTemplateRepo: &serialTplRepo{byStage: tplByStage},
	}
}

func depsByName(spec *runnerapi.PipelineRunSpec) map[string][]string {
	out := map[string][]string{}
	for _, tk := range spec.Tasks {
		out[tk.Name] = tk.DependsOn
	}
	return out
}

func TestBuildSpec_SerialStageChainsTasks(t *testing.T) {
	pid, s1 := uuid.New(), uuid.New()
	svc := newSerialTestSvc(t,
		[]pipelinemodels.PipelineStage{{ID: s1, PipelineID: pid, Name: "build", Sequence: 1, ExecutionMode: pipelinemodels.ExecutionModeSerial}},
		map[uuid.UUID][]pipelinemodels.PipelineTaskTemplate{s1: {serialTask("a"), serialTask("b"), serialTask("c")}},
	)

	spec, err := svc.buildSpec(pid, &models.TriggerRequest{TriggeredBy: "test"})
	if err != nil {
		t.Fatalf("buildSpec: %v", err)
	}
	deps := depsByName(spec)
	if len(deps["a"]) != 0 {
		t.Fatalf("first serial task must have no deps, got %v", deps["a"])
	}
	if len(deps["b"]) != 1 || deps["b"][0] != "a" {
		t.Fatalf("second serial task must depend on [a], got %v", deps["b"])
	}
	if len(deps["c"]) != 1 || deps["c"][0] != "b" {
		t.Fatalf("third serial task must depend on [b], got %v", deps["c"])
	}
}

func TestBuildSpec_ParallelStageHasNoIntraStageDeps(t *testing.T) {
	pid, s1 := uuid.New(), uuid.New()
	svc := newSerialTestSvc(t,
		[]pipelinemodels.PipelineStage{{ID: s1, PipelineID: pid, Name: "build", Sequence: 1, ExecutionMode: pipelinemodels.ExecutionModeParallel}},
		map[uuid.UUID][]pipelinemodels.PipelineTaskTemplate{s1: {serialTask("a"), serialTask("b")}},
	)

	spec, err := svc.buildSpec(pid, &models.TriggerRequest{TriggeredBy: "test"})
	if err != nil {
		t.Fatalf("buildSpec: %v", err)
	}
	deps := depsByName(spec)
	for _, name := range []string{"a", "b"} {
		if len(deps[name]) != 0 {
			t.Fatalf("parallel task %q must have no intra-stage deps, got %v", name, deps[name])
		}
	}
}

func TestBuildSpec_CrossStageDepWithSerialFirstStage(t *testing.T) {
	// 跨阶段语义不变：第二阶段（parallel）每个任务依赖第一阶段全部任务；
	// 第一阶段自身是 serial 时内部还要成链。
	pid, s1, s2 := uuid.New(), uuid.New(), uuid.New()
	svc := newSerialTestSvc(t,
		[]pipelinemodels.PipelineStage{
			{ID: s1, PipelineID: pid, Name: "build", Sequence: 1, ExecutionMode: pipelinemodels.ExecutionModeSerial},
			{ID: s2, PipelineID: pid, Name: "deploy", Sequence: 2, ExecutionMode: pipelinemodels.ExecutionModeParallel},
		},
		map[uuid.UUID][]pipelinemodels.PipelineTaskTemplate{
			s1: {serialTask("a"), serialTask("b")},
			s2: {serialTask("x"), serialTask("y")},
		},
	)

	spec, err := svc.buildSpec(pid, &models.TriggerRequest{TriggeredBy: "test"})
	if err != nil {
		t.Fatalf("buildSpec: %v", err)
	}
	deps := depsByName(spec)
	if len(deps["x"]) != 2 {
		t.Fatalf("cross-stage task must depend on all first-stage tasks, got %v", deps["x"])
	}
	if len(deps["a"]) != 0 || len(deps["b"]) != 1 || deps["b"][0] != "a" {
		t.Fatalf("first stage must stay chained: a=%v b=%v", deps["a"], deps["b"])
	}
}

// --- StageProgress（derive-on-read 聚合，DATA-MODEL §6.5）------------------

// 只覆盖被调用的方法：接口嵌入让未实现的方法保持 nil（不会被走到）。
type spRunStore struct {
	PipelineRunStore
	run *models.PipelineRun
}

func (f *spRunStore) GetByID(uuid.UUID) (*models.PipelineRun, error) { return f.run, nil }

type spTaskStore struct {
	TaskRunStore
	tasks []models.TaskRun
}

func (f *spTaskStore) ListByPipelineRunID(uuid.UUID) ([]models.TaskRun, error) { return f.tasks, nil }

func TestStageProgress_AggregatesByStageName(t *testing.T) {
	pid := uuid.New()
	run := &models.PipelineRun{ID: uuid.New(), PipelineID: pid, Phase: "Running"}
	tasks := []models.TaskRun{
		{TaskName: "a", StageName: "build", Phase: runnerapi.TaskRunSucceeded},
		{TaskName: "b", StageName: "build", Phase: runnerapi.TaskRunFailed},
		{TaskName: "x", StageName: "deploy", Phase: runnerapi.TaskRunRunning},
	}
	svc := &PipelineRunService{
		repo:     &spRunStore{run: run},
		taskRepo: &spTaskStore{tasks: tasks},
		stageRepo: &serialStageRepo{stages: []pipelinemodels.PipelineStage{
			{PipelineID: pid, Name: "build", Sequence: 1, ExecutionMode: pipelinemodels.ExecutionModeSerial},
			{PipelineID: pid, Name: "deploy", Sequence: 2, ExecutionMode: pipelinemodels.ExecutionModeParallel},
		}},
	}

	_, rows, err := svc.StageProgress(run.ID)
	if err != nil {
		t.Fatalf("StageProgress: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("want 2 stage rows, got %d", len(rows))
	}
	if rows[0].Name != "build" || rows[0].Done != 2 || rows[0].Total != 2 || rows[0].Status != "failed" {
		t.Fatalf("build row wrong: %+v", rows[0])
	}
	if rows[0].ExecutionMode != pipelinemodels.ExecutionModeSerial {
		t.Fatalf("build row must carry definition executionMode, got %q", rows[0].ExecutionMode)
	}
	if rows[1].Name != "deploy" || rows[1].Done != 0 || rows[1].Total != 1 || rows[1].Status != "running" {
		t.Fatalf("deploy row wrong: %+v", rows[1])
	}
}

func TestStageProgress_DefinitionDriftBecomesSyntheticRow(t *testing.T) {
	pid := uuid.New()
	run := &models.PipelineRun{ID: uuid.New(), PipelineID: pid}
	tasks := []models.TaskRun{
		{TaskName: "ghost", StageName: "gone-stage", Phase: runnerapi.TaskRunSucceeded},
	}
	svc := &PipelineRunService{
		repo:     &spRunStore{run: run},
		taskRepo: &spTaskStore{tasks: tasks},
		stageRepo: &serialStageRepo{stages: []pipelinemodels.PipelineStage{
			{PipelineID: pid, Name: "build", Sequence: 1, ExecutionMode: pipelinemodels.ExecutionModeParallel},
		}},
	}

	_, rows, err := svc.StageProgress(run.ID)
	if err != nil {
		t.Fatalf("StageProgress: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("want 2 rows (def + drift), got %d", len(rows))
	}
	if rows[1].Name != "gone-stage" || rows[1].Sequence != 0 || rows[1].Status != "succeeded" {
		t.Fatalf("drift row wrong: %+v", rows[1])
	}
}

func TestStageProgress_EmptyRunAllPending(t *testing.T) {
	pid := uuid.New()
	run := &models.PipelineRun{ID: uuid.New(), PipelineID: pid}
	svc := &PipelineRunService{
		repo:     &spRunStore{run: run},
		taskRepo: &spTaskStore{},
		stageRepo: &serialStageRepo{stages: []pipelinemodels.PipelineStage{
			{PipelineID: pid, Name: "build", Sequence: 1, ExecutionMode: pipelinemodels.ExecutionModeParallel},
		}},
	}

	_, rows, err := svc.StageProgress(run.ID)
	if err != nil {
		t.Fatalf("StageProgress: %v", err)
	}
	if len(rows) != 1 || rows[0].Status != "pending" || rows[0].Total != 0 {
		t.Fatalf("empty run must yield pending zero rows, got %+v", rows)
	}
}
