package models

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"

	runnerapi "github.com/rouroumaibing/software-distribution-platform-runner/api/v1alpha1"
)

// 覆盖 C-09 的对比规则（纯逻辑，无 DB）。这些是"版本对比"功能的**判定核心**：
// 服务层只负责取两条快照，改不改、哪里改了全由这里的规则决定。

var diffNow = time.Unix(1700000000, 0)

func snap(stages ...SnapshotStage) Snapshot {
	return Snapshot{PipelineID: [16]byte{1}, Name: "p", Kind: "build", Stages: stages}
}

func stage(name string, seq int, mode string, tasks ...SnapshotTask) SnapshotStage {
	if mode == "" {
		mode = ExecutionModeParallel
	}
	return SnapshotStage{Name: name, Sequence: seq, ExecutionMode: mode, Tasks: tasks}
}

func task(name string, order int) SnapshotTask {
	return SnapshotTask{Name: name, Type: runnerapi.TaskTypeBuild, DisplayOrder: order}
}

func TestDiffSnapshots_IdenticalIsQuiet(t *testing.T) {
	a := snap(stage("构建", 1, "", task("c", 1)))
	b := snap(stage("构建", 1, "", task("c", 1)))

	d := DiffSnapshots(a, b, 1, 2, diffNow, diffNow)
	if !d.Identical {
		t.Fatalf("same body must be identical, got %+v", d)
	}
	if len(d.Stages) != 0 || len(d.Tasks) != 0 {
		t.Fatalf("identical diff must carry no entries, got %+v", d)
	}
}

func TestDiffSnapshots_StageAddRemoveModify(t *testing.T) {
	from := snap(
		stage("构建", 1, "", task("c", 1)),
		stage("测试", 2, "", task("t", 1)),
	)
	to := snap(
		// 构建：sequence 从 1 变 3，模式 parallel → serial
		stage("构建", 3, ExecutionModeSerial, task("c", 1)),
		// 部署：新增
		stage("部署", 2, "", task("d", 1)),
	)

	d := DiffSnapshots(from, to, 4, 5, diffNow, diffNow)

	if d.FromVersion != 4 || d.ToVersion != 5 {
		t.Fatalf("version numbers not carried: %d → %d", d.FromVersion, d.ToVersion)
	}
	if d.Summary.StagesAdded != 1 || d.Summary.StagesRemoved != 1 || d.Summary.StagesModified != 1 {
		t.Fatalf("stage summary wrong: %+v", d.Summary)
	}
	if d.Summary.TasksAdded != 1 || d.Summary.TasksRemoved != 1 {
		t.Fatalf("task summary wrong: %+v", d.Summary)
	}

	byName := map[string]StageDiff{}
	for _, s := range d.Stages {
		byName[s.Name+"="+string(s.Change)] = s
	}
	modified, ok := byName["构建=modified"]
	if !ok {
		t.Fatalf("构建 should be modified, got %+v", d.Stages)
	}
	if len(modified.Fields) != 2 {
		t.Fatalf("want sequence + executionMode changes, got %+v", modified.Fields)
	}
	if _, ok := byName["测试=removed"]; !ok {
		t.Fatalf("测试 should be removed, got %+v", d.Stages)
	}
	if _, ok := byName["部署=added"]; !ok {
		t.Fatalf("部署 should be added, got %+v", d.Stages)
	}
}

// 新增 / 删除阶段的**子任务**也要显式列出：评审者看到"阶段已新增"时还想知道
// 这一版带进来哪些任务，不该被迫再去拉另一版。
func TestDiffSnapshots_TasksOfAddedAndRemovedStagesAreListed(t *testing.T) {
	from := snap(stage("构建", 1, "", task("c", 1)))
	to := snap(stage("部署", 1, "", task("apply", 1), task("verify", 2)))

	d := DiffSnapshots(from, to, 1, 2, diffNow, diffNow)

	if d.Summary.TasksAdded != 2 || d.Summary.TasksRemoved != 1 {
		t.Fatalf("want 2 added + 1 removed tasks, got %+v", d.Summary)
	}
	seen := map[string]bool{}
	for _, td := range d.Tasks {
		seen[td.Stage+"/"+td.Name+"="+string(td.Change)] = true
	}
	for _, want := range []string{"部署/apply=added", "部署/verify=added", "构建/c=removed"} {
		if !seen[want] {
			t.Fatalf("missing task diff %q in %v", want, seen)
		}
	}
}

func TestDiffSnapshots_TaskFieldChanges(t *testing.T) {
	before := task("c", 1)
	before.Image = "golang:1.21"
	before.Command = json.RawMessage(`["go","build"]`)
	before.TimeoutSeconds = 60
	before.ScriptArgs = json.RawMessage(`[]`)

	after := task("c", 2)
	after.Image = "golang:1.22"
	after.Command = json.RawMessage(`["go","test"]`)
	after.TimeoutSeconds = 300
	after.ScriptArgs = json.RawMessage(`[]`)

	d := DiffSnapshots(
		snap(stage("构建", 1, "", before)),
		snap(stage("构建", 1, "", after)),
		1, 2, diffNow, diffNow,
	)

	if len(d.Tasks) != 1 {
		t.Fatalf("want 1 task diff, got %+v", d.Tasks)
	}
	fields := map[string]FieldChange{}
	for _, fc := range d.Tasks[0].Fields {
		fields[fc.Field] = fc
	}
	for _, want := range []string{"displayOrder", "image", "command", "timeoutSeconds"} {
		if _, ok := fields[want]; !ok {
			t.Fatalf("missing field change %q in %+v", want, d.Tasks[0].Fields)
		}
	}
	if fields["image"].From != "golang:1.21" || fields["image"].To != "golang:1.22" {
		t.Fatalf("image change wrong: %+v", fields["image"])
	}
}

// 纯外观差异（键序 / 空白）不算改动 —— 否则一次无意义的重新保存会把每个
// JSON 字段都标成"改过"。
func TestDiffSnapshots_IgnoresCosmeticJSONDifference(t *testing.T) {
	before := task("c", 1)
	before.RetryPolicy = json.RawMessage(`{"maxRetries":2,"backoff":"30s"}`)
	after := task("c", 1)
	after.RetryPolicy = json.RawMessage("{\n  \"backoff\": \"30s\",\n  \"maxRetries\": 2\n}")

	d := DiffSnapshots(
		snap(stage("构建", 1, "", before)),
		snap(stage("构建", 1, "", after)),
		1, 2, diffNow, diffNow,
	)
	if !d.Identical {
		t.Fatalf("key order / whitespace alone must not count as a change, got %+v", d.Tasks)
	}
}

// 坏 JSON 不能被静默归一成"没变"：那是真实的数据损坏，必须显式暴露。
func TestDiffSnapshots_CorruptJSONSurfacesAsChange(t *testing.T) {
	before := task("c", 1)
	before.Command = json.RawMessage(`["go","build"]`)
	after := task("c", 1)
	after.Command = json.RawMessage(`["go",`)

	d := DiffSnapshots(
		snap(stage("构建", 1, "", before)),
		snap(stage("构建", 1, "", after)),
		1, 2, diffNow, diffNow,
	)
	if d.Identical || len(d.Tasks) != 1 {
		t.Fatalf("corrupt JSON must show up as a change, got identical=%v tasks=%+v", d.Identical, d.Tasks)
	}
}

// 同名子任务分处不同阶段时不能被合并比较：身份是 (阶段, 任务) 而不是任务名。
func TestDiffSnapshots_SameTaskNameInDifferentStagesIsNotMerged(t *testing.T) {
	from := snap(stage("构建", 1, "", task("build", 1)))
	to := snap(stage("构建", 1, "", task("build", 1)), stage("测试", 2, "", task("build", 1)))

	d := DiffSnapshots(from, to, 1, 2, diffNow, diffNow)
	if d.Summary.TasksAdded != 1 || d.Summary.TasksRemoved != 0 {
		t.Fatalf("a new stage with a same-named task is an addition, got %+v", d.Summary)
	}
	if d.Tasks[0].Stage != "测试" {
		t.Fatalf("the added task belongs to 测试, got %+v", d.Tasks[0])
	}
}

func TestDiffSnapshots_OutputIsDeterministic(t *testing.T) {
	from := snap(stage("b", 1, "", task("z", 1)), stage("a", 1, "", task("y", 1)))
	to := snap(stage("c", 1, "", task("x", 1)), stage("d", 1, "", task("w", 1)))

	first := DiffSnapshots(from, to, 1, 2, diffNow, diffNow)
	for i := 0; i < 5; i++ {
		again := DiffSnapshots(from, to, 1, 2, diffNow, diffNow)
		if !reflect.DeepEqual(again.Stages, first.Stages) {
			t.Fatalf("stage diffs are not stable: %v vs %v", again.Stages, first.Stages)
		}
		if !reflect.DeepEqual(again.Tasks, first.Tasks) {
			t.Fatalf("task diffs are not stable: %v vs %v", again.Tasks, first.Tasks)
		}
	}
}
