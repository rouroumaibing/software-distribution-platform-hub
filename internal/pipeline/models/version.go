package models

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"

	runnerapi "github.com/rouroumaibing/software-distribution-platform-runner/api/v1alpha1"
)

// PipelineVersion is an immutable snapshot of a pipeline's stages+tasks,
// captured every time a structural edit is saved. Mirrors what the hub
// materializes into runnerapi.PipelineRunSpec.Tasks[] at trigger time.
type PipelineVersion struct {
	ID         uuid.UUID      `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"id"`
	PipelineID uuid.UUID      `gorm:"type:uuid;not null;index;uniqueIndex:idx_pipeline_versions_pipeline_version" json:"pipelineId"`
	Version    int            `gorm:"not null;uniqueIndex:idx_pipeline_versions_pipeline_version" json:"version"`
	Snapshot   datatypes.JSON `gorm:"not null" json:"snapshot"`
	CreatedBy  string         `gorm:"size:128" json:"createdBy,omitempty"`
	CreatedAt  time.Time      `json:"createdAt"`
}

func (PipelineVersion) TableName() string { return "pipeline_versions" }

// Snapshot is the decoded body of PipelineVersion.Snapshot.
//
// 刻意**不含** Version 字段（它在行上是列）：快照体只描述"定义长什么样"，
// 这样"体没变就不必记新版本"的去重判定才成立 —— 否则每次发布都会因为版本号
// 自增而必然产生一个"不同"的体，去重形同虚设。
//
// 元信息（name / kind / description）与结构（stages/tasks）都在体内，因此
// 一次改名与一次改阶段一样会被记成新版本。
type Snapshot struct {
	PipelineID  uuid.UUID       `json:"pipelineId"`
	Name        string          `json:"name"`
	Kind        string          `json:"kind"`
	Description string          `json:"description,omitempty"`
	Stages      []SnapshotStage `json:"stages"`
}

// SnapshotStage is one stage plus the task templates grouped under it, in
// Sequence / DisplayOrder order.
type SnapshotStage struct {
	Name          string         `json:"name"`
	Sequence      int            `json:"sequence"`
	ExecutionMode string         `json:"executionMode"`
	Tasks         []SnapshotTask `json:"tasks"`
}

// SnapshotTask is a full-fidelity copy of a pipeline_task_templates row. Every
// JSON column is kept as json.RawMessage so a rollback re-materializes the
// exact bytes the author saved, not a re-encoded approximation.
type SnapshotTask struct {
	Name           string                     `json:"name"`
	Type           runnerapi.PipelineTaskType `json:"type"`
	DisplayOrder   int                        `json:"displayOrder"`
	Image          string                     `json:"image,omitempty"`
	ScriptPath     string                     `json:"scriptPath,omitempty"`
	ScriptArgs     json.RawMessage            `json:"scriptArgs,omitempty"`
	Command        json.RawMessage            `json:"command,omitempty"`
	Args           json.RawMessage            `json:"args,omitempty"`
	Produces       json.RawMessage            `json:"produces,omitempty"`
	Consumes       json.RawMessage            `json:"consumes,omitempty"`
	ReleaseConfig  json.RawMessage            `json:"releaseConfig,omitempty"`
	RolloutConfig  json.RawMessage            `json:"rolloutConfig,omitempty"`
	ApprovalConfig json.RawMessage            `json:"approvalConfig,omitempty"`
	RetryPolicy    json.RawMessage            `json:"retryPolicy,omitempty"`
	TimeoutSeconds int                        `json:"timeoutSeconds"`
}

// BuildSnapshot assembles the snapshot body from the pipeline row plus its
// (already ordered) stages and per-stage task templates. Pure: it reads no DB
// and no clock, so the shape it produces is unit-testable on its own.
//
// tasksByStage is keyed by stage id; a stage with no entry gets an empty task
// list rather than nil, so two "structurally equal" snapshots never differ by
// a JSON null-vs-[] artefact.
func BuildSnapshot(
	p *Pipeline,
	stages []PipelineStage,
	tasksByStage map[uuid.UUID][]PipelineTaskTemplate,
) Snapshot {
	snap := Snapshot{
		PipelineID:  p.ID,
		Name:        p.Name,
		Kind:        p.Kind,
		Description: p.Description,
		Stages:      make([]SnapshotStage, 0, len(stages)),
	}
	for _, st := range stages {
		out := SnapshotStage{
			Name:          st.Name,
			Sequence:      st.Sequence,
			ExecutionMode: st.ExecutionMode,
			Tasks:         make([]SnapshotTask, 0),
		}
		for _, t := range tasksByStage[st.ID] {
			out.Tasks = append(out.Tasks, SnapshotTask{
				Name:           t.Name,
				Type:           t.Type,
				DisplayOrder:   t.DisplayOrder,
				Image:          t.Image,
				ScriptPath:     t.ScriptPath,
				ScriptArgs:     rawOrEmpty(t.ScriptArgs),
				Command:        rawOrEmpty(t.Command),
				Args:           rawOrEmpty(t.Args),
				Produces:       rawOrEmpty(t.Produces),
				Consumes:       rawOrEmpty(t.Consumes),
				ReleaseConfig:  rawOrEmpty(t.ReleaseConfig),
				RolloutConfig:  rawOrEmpty(t.RolloutConfig),
				ApprovalConfig: rawOrEmpty(t.ApprovalConfig),
				RetryPolicy:    rawOrEmpty(t.RetryPolicy),
				TimeoutSeconds: t.TimeoutSeconds,
			})
		}
		snap.Stages = append(snap.Stages, out)
	}
	return snap
}

func rawOrEmpty(v datatypes.JSON) json.RawMessage {
	if len(v) == 0 {
		return nil
	}
	return json.RawMessage(v)
}

// DecodeSnapshot parses a stored PipelineVersion.Snapshot body.
func DecodeSnapshot(raw datatypes.JSON) (Snapshot, error) {
	var s Snapshot
	if len(raw) == 0 {
		return s, fmt.Errorf("empty snapshot")
	}
	if err := json.Unmarshal(raw, &s); err != nil {
		return s, fmt.Errorf("decode snapshot: %w", err)
	}
	return s, nil
}

// ---------- diff -----------------------------------------------------------

// ChangeType enumerates how a structural element differs between two versions.
type ChangeType string

const (
	ChangeAdded    ChangeType = "added"
	ChangeRemoved  ChangeType = "removed"
	ChangeModified ChangeType = "modified"
)

// FieldChange is one changed scalar/JSON field, rendered as strings so the
// console can print a diff row without knowing the field's Go type.
type FieldChange struct {
	Field string `json:"field"`
	From  string `json:"from"`
	To    string `json:"to"`
}

// StageDiff is one stage-level difference, identified by stage name.
type StageDiff struct {
	Name   string        `json:"name"`
	Change ChangeType    `json:"change"`
	Fields []FieldChange `json:"fields,omitempty"`
}

// TaskDiff is one task-level difference. Identity is (stage name, task name) —
// the same natural key the API uses, so a task that moved between stages shows
// up as a modification of its owning stage.
type TaskDiff struct {
	Stage  string        `json:"stage"`
	Name   string        `json:"name"`
	Change ChangeType    `json:"change"`
	Fields []FieldChange `json:"fields,omitempty"`
}

// DiffSummary carries pre-computed counts. The console renders these verbatim
// instead of re-counting the arrays (one source of truth for the numbers).
type DiffSummary struct {
	StagesAdded    int `json:"stagesAdded"`
	StagesRemoved  int `json:"stagesRemoved"`
	StagesModified int `json:"stagesModified"`
	TasksAdded     int `json:"tasksAdded"`
	TasksRemoved   int `json:"tasksRemoved"`
	TasksModified  int `json:"tasksModified"`
}

// SnapshotDiff is the result of comparing two snapshots (from → to).
type SnapshotDiff struct {
	FromVersion   int         `json:"fromVersion"`
	ToVersion     int         `json:"toVersion"`
	FromCreatedAt time.Time   `json:"fromCreatedAt"`
	ToCreatedAt   time.Time   `json:"toCreatedAt"`
	Stages        []StageDiff `json:"stages"`
	Tasks         []TaskDiff  `json:"tasks"`
	Summary       DiffSummary `json:"summary"`
	// Identical is true when no stage/task/field differs.
	Identical bool `json:"identical"`
}

// DiffSnapshots compares two snapshot bodies. Pure — no DB, no clock — so the
// comparison rules are unit-testable and the service layer stays thin.
//
// 身份取**名字**而不是 id：版本之间的行 id 本来就不同（每次回滚都会重新
// materialize 出新的 stage/task 行），拿 id 比会把"同一个阶段"全判成删+增。
// 名字恰是 API 层的自然键（pipeline 内 stage 名、stage 内 task 名）。
func DiffSnapshots(from, to Snapshot, fromVersion, toVersion int, fromAt, toAt time.Time) SnapshotDiff {
	d := SnapshotDiff{
		FromVersion:   fromVersion,
		ToVersion:     toVersion,
		FromCreatedAt: fromAt,
		ToCreatedAt:   toAt,
		Stages:        []StageDiff{},
		Tasks:         []TaskDiff{},
	}

	fromStages := map[string]SnapshotStage{}
	for _, s := range from.Stages {
		fromStages[s.Name] = s
	}
	toStages := map[string]SnapshotStage{}
	for _, s := range to.Stages {
		toStages[s.Name] = s
	}

	for _, s := range to.Stages {
		prev, ok := fromStages[s.Name]
		if !ok {
			d.Stages = append(d.Stages, StageDiff{Name: s.Name, Change: ChangeAdded})
			d.Summary.StagesAdded++
			continue
		}
		if fields := stageFieldChanges(prev, s); len(fields) > 0 {
			d.Stages = append(d.Stages, StageDiff{Name: s.Name, Change: ChangeModified, Fields: fields})
			d.Summary.StagesModified++
		}
	}
	for _, s := range from.Stages {
		if _, ok := toStages[s.Name]; ok {
			continue
		}
		d.Stages = append(d.Stages, StageDiff{Name: s.Name, Change: ChangeRemoved})
		d.Summary.StagesRemoved++
	}

	// Task-level diffs are reported for **every** stage, including added /
	// removed ones: a reviewer reading "阶段 构建 已新增" also wants to see
	// which tasks come with it, so the body stays explicit rather than
	// requiring the reader to fetch the other version to find out.
	for _, s := range to.Stages {
		prev, inFrom := fromStages[s.Name]
		prevTasks := indexTasks(prev.Tasks)
		for _, t := range s.Tasks {
			old, ok := prevTasks[t.Name]
			switch {
			case !inFrom || !ok:
				d.Tasks = append(d.Tasks, TaskDiff{Stage: s.Name, Name: t.Name, Change: ChangeAdded})
				d.Summary.TasksAdded++
			default:
				if fields := taskFieldChanges(old, t); len(fields) > 0 {
					d.Tasks = append(d.Tasks, TaskDiff{Stage: s.Name, Name: t.Name, Change: ChangeModified, Fields: fields})
					d.Summary.TasksModified++
				}
			}
		}
	}
	for _, s := range from.Stages {
		next, inTo := toStages[s.Name]
		nextTasks := indexTasks(next.Tasks)
		for _, t := range s.Tasks {
			if inTo {
				if _, ok := nextTasks[t.Name]; ok {
					continue
				}
			}
			d.Tasks = append(d.Tasks, TaskDiff{Stage: s.Name, Name: t.Name, Change: ChangeRemoved})
			d.Summary.TasksRemoved++
		}
	}

	sortDiffs(&d)
	d.Identical = len(d.Stages) == 0 && len(d.Tasks) == 0
	return d
}

func indexTasks(tasks []SnapshotTask) map[string]SnapshotTask {
	out := make(map[string]SnapshotTask, len(tasks))
	for _, t := range tasks {
		out[t.Name] = t
	}
	return out
}

func stageFieldChanges(from, to SnapshotStage) []FieldChange {
	var out []FieldChange
	if from.Sequence != to.Sequence {
		out = append(out, FieldChange{Field: "sequence", From: itoa(from.Sequence), To: itoa(to.Sequence)})
	}
	if from.ExecutionMode != to.ExecutionMode {
		out = append(out, FieldChange{Field: "executionMode", From: from.ExecutionMode, To: to.ExecutionMode})
	}
	return out
}

func taskFieldChanges(from, to SnapshotTask) []FieldChange {
	var out []FieldChange
	add := func(field, a, b string) {
		if a != b {
			out = append(out, FieldChange{Field: field, From: a, To: b})
		}
	}
	add("type", string(from.Type), string(to.Type))
	add("displayOrder", itoa(from.DisplayOrder), itoa(to.DisplayOrder))
	add("image", from.Image, to.Image)
	add("scriptPath", from.ScriptPath, to.ScriptPath)
	add("timeoutSeconds", itoa(from.TimeoutSeconds), itoa(to.TimeoutSeconds))
	// JSON columns compare on their **canonical** encoding so a purely
	// cosmetic re-serialization (key order / whitespace) is not reported as a
	// change — otherwise a no-op save would look like an edit of every field.
	add("scriptArgs", canonical(from.ScriptArgs), canonical(to.ScriptArgs))
	add("command", canonical(from.Command), canonical(to.Command))
	add("args", canonical(from.Args), canonical(to.Args))
	add("produces", canonical(from.Produces), canonical(to.Produces))
	add("consumes", canonical(from.Consumes), canonical(to.Consumes))
	add("retryPolicy", canonical(from.RetryPolicy), canonical(to.RetryPolicy))
	add("releaseConfig", canonical(from.ReleaseConfig), canonical(to.ReleaseConfig))
	add("rolloutConfig", canonical(from.RolloutConfig), canonical(to.RolloutConfig))
	add("approvalConfig", canonical(from.ApprovalConfig), canonical(to.ApprovalConfig))
	return out
}

// sortDiffs orders the diff slices by (stage appearance order, then name) so
// two runs of the same comparison produce byte-identical output. Task order
// follows the stage order of the "to" side where possible.
func sortDiffs(d *SnapshotDiff) {
	sort.SliceStable(d.Stages, func(i, j int) bool {
		if d.Stages[i].Name != d.Stages[j].Name {
			return d.Stages[i].Name < d.Stages[j].Name
		}
		return changeRank(d.Stages[i].Change) < changeRank(d.Stages[j].Change)
	})
	sort.SliceStable(d.Tasks, func(i, j int) bool {
		if d.Tasks[i].Stage != d.Tasks[j].Stage {
			return d.Tasks[i].Stage < d.Tasks[j].Stage
		}
		if d.Tasks[i].Name != d.Tasks[j].Name {
			return d.Tasks[i].Name < d.Tasks[j].Name
		}
		return changeRank(d.Tasks[i].Change) < changeRank(d.Tasks[j].Change)
	})
}

func changeRank(c ChangeType) int {
	switch c {
	case ChangeAdded:
		return 0
	case ChangeModified:
		return 1
	default:
		return 2
	}
}

func itoa(n int) string { return fmt.Sprintf("%d", n) }

// CanonicalJSON renders arbitrary JSON in a stable form (object keys sorted,
// no insignificant whitespace). Exported for tests and for comparing two
// stored blobs without decoding them into a struct first.
//
// 非法 JSON / 空值原样返回字符串——它们是"坏数据"，不该在此处被静默归一成 {}，
// 否则一次真实的数据损坏会被 diff 判成"没改"。
func CanonicalJSON(raw []byte) string {
	if len(raw) == 0 {
		return ""
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return string(raw)
	}
	out, err := json.Marshal(v)
	if err != nil {
		return string(raw)
	}
	return string(out)
}

func canonical(raw json.RawMessage) string { return CanonicalJSON(raw) }
