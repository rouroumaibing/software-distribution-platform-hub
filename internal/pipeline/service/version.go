package service

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"
	"gorm.io/gorm"

	"github.com/rouroumaibing/software-distribution-platform-hub/internal/common"
	"github.com/rouroumaibing/software-distribution-platform-hub/internal/pipeline/models"
)

// errVersionNotFound / errBadVersion 的编码沿用 pipeline 域（ERR.08…）：
// 版本是流水线聚合内的一部分，不是独立资源。
var (
	errVersionNotFound = common.DomainError(common.KindPipeline, http.StatusNotFound, 3, "pipeline version not found")
	errBadVersion      = common.DomainError(common.KindPipeline, http.StatusBadRequest, 2,
		"invalid version (want a positive integer)")
)

// VersionPublisher is the hook StageService / TaskTemplateService call after a
// successful structural mutation (stage or task created / updated / deleted).
//
// 抽成接口而不是直接依赖 *PipelineVersionService：让"结构改动必须记版本"这条
// 不变量能在不接 DB 的情况下被单测钉住（与仓内 StageStore / TemplateCascade 同手法）。
type VersionPublisher interface {
	Publish(pipelineID uuid.UUID, createdBy string) error
}

// VersionRepo is the version-table surface the service needs.
type VersionRepo interface {
	Create(*models.PipelineVersion) error
	ListByPipelineID(pipelineID uuid.UUID, limit int) ([]models.PipelineVersion, error)
	GetByVersion(pipelineID uuid.UUID, version int) (*models.PipelineVersion, error)
	Latest(pipelineID uuid.UUID) (*models.PipelineVersion, error)
}

// VersionPipelineStore is the pipeline slice needed to read metadata and bump
// pipelines.version.
type VersionPipelineStore interface {
	GetByID(uuid.UUID) (*models.Pipeline, error)
	Update(*models.Pipeline) error
}

// VersionStageStore is the stage slice needed to read and re-materialize the
// structure. *repository.StageRepository satisfies it as-is.
type VersionStageStore interface {
	Create(*models.PipelineStage) error
	GetByID(uuid.UUID) (*models.PipelineStage, error)
	Update(*models.PipelineStage) error
	Delete(uuid.UUID) error
	ListByPipelineID(uuid.UUID) ([]models.PipelineStage, error)
}

// VersionTaskStore is the task-template slice needed to read and
// re-materialize the structure.
type VersionTaskStore interface {
	Create(*models.PipelineTaskTemplate) error
	ListByStageID(uuid.UUID) ([]models.PipelineTaskTemplate, error)
	DeleteByStageID(uuid.UUID) error
}

// PipelineVersionService owns the pipeline definition's history: it snapshots
// the structure on every change, answers list/get/compare, and re-materializes
// a past version on rollback.
//
// 单一写者：pipelines.version 的自增与 pipeline_versions 的写入都只在这里发生。
// 早前 `PipelineService.Update` 自己 +1、结构改动却完全不记版本，结果是
// "版本号会跳、但快照永远缺席"——历史运行记录的 PipelineVersion 指向一个
// 查不到内容的号码。把它收进一个服务后，"自增"与"留档"不可能再各走各的。
type PipelineVersionService struct {
	pipelines VersionPipelineStore
	stages    VersionStageStore
	tasks     VersionTaskStore
	versions  VersionRepo
}

func NewPipelineVersionService(
	pipelines VersionPipelineStore,
	stages VersionStageStore,
	tasks VersionTaskStore,
	versions VersionRepo,
) *PipelineVersionService {
	return &PipelineVersionService{pipelines: pipelines, stages: stages, tasks: tasks, versions: versions}
}

// Publish snapshots the pipeline's current definition and, if the body differs
// from the newest recorded snapshot, records it under a bumped
// pipelines.version.
//
// 去重（体相同即不动）不是优化而是语义：console 的 [保存] 会对**每个**阶段与
// 子任务发一次 PUT（附 D.4 的调用序列），其中绝大多数字段并未改动。若每次都记
// 一版，一次"保存"就会凭空造出十几版历史，把真正有内容的那一版埋掉。
func (s *PipelineVersionService) Publish(pipelineID uuid.UUID, createdBy string) error {
	snap, err := s.currentSnapshot(pipelineID)
	if err != nil {
		return err
	}
	body, err := json.Marshal(snap)
	if err != nil {
		return fmt.Errorf("marshal snapshot: %w", err)
	}

	if latest, lerr := s.versions.Latest(pipelineID); lerr == nil && latest != nil {
		if string(latest.Snapshot) == string(body) {
			return nil // 定义未变：不记新版本，也不动版本号
		}
	} else if !errors.Is(lerr, gorm.ErrRecordNotFound) {
		return lerr
	}

	p, err := s.pipelines.GetByID(pipelineID)
	if err != nil {
		return err
	}
	p.Version = p.Version + 1
	if err := s.pipelines.Update(p); err != nil {
		return err
	}
	return s.versions.Create(&models.PipelineVersion{
		PipelineID: pipelineID,
		Version:    p.Version,
		Snapshot:   datatypes.JSON(body),
		CreatedBy:  createdBy,
	})
}

// currentSnapshot reads the live definition and assembles its snapshot body.
func (s *PipelineVersionService) currentSnapshot(pipelineID uuid.UUID) (models.Snapshot, error) {
	p, err := s.pipelines.GetByID(pipelineID)
	if err != nil {
		return models.Snapshot{}, err
	}
	stages, err := s.stages.ListByPipelineID(pipelineID)
	if err != nil {
		return models.Snapshot{}, err
	}
	byStage := make(map[uuid.UUID][]models.PipelineTaskTemplate, len(stages))
	for _, st := range stages {
		tasks, terr := s.tasks.ListByStageID(st.ID)
		if terr != nil {
			return models.Snapshot{}, terr
		}
		byStage[st.ID] = tasks
	}
	return models.BuildSnapshot(p, stages, byStage), nil
}

// VersionSummary is the list-view of a snapshot: everything the console's
// history panel needs, without the (potentially large) snapshot body.
type VersionSummary struct {
	Version   int       `json:"version"`
	CreatedBy string    `json:"createdBy,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
	Stages    int       `json:"stages"`
	Tasks     int       `json:"tasks"`
	// IdenticalToPrevious is true when this version's body is byte-identical
	// to the next-older one, nil when that older version is outside the
	// requested window (so the UI can render "未知" instead of guessing "否").
	IdenticalToPrevious *bool `json:"identicalToPrevious,omitempty"`
	// IsCurrent marks the newest version — the one a trigger would snapshot.
	IsCurrent bool `json:"isCurrent"`
}

// DefaultVersionListLimit / MaxVersionListLimit bound the history window the
// list endpoint returns. An out-of-range limit is a 400, not a silent clamp —
// the search module's ?limit= follows the same rule.
const (
	DefaultVersionListLimit = 50
	MaxVersionListLimit     = 200
)

// List returns a pipeline's version history (newest first). limit<=0 means
// DefaultVersionListLimit.
func (s *PipelineVersionService) List(pipelineID uuid.UUID, limit int) ([]VersionSummary, error) {
	if limit <= 0 {
		limit = DefaultVersionListLimit
	}
	if limit > MaxVersionListLimit {
		return nil, common.DomainError(common.KindPipeline, http.StatusBadRequest, 4,
			fmt.Sprintf("limit exceeds %d", MaxVersionListLimit))
	}
	// 多取一条：判断"最后一版与上一版是否相同"需要窗口外的那一条，用它来决定
	// 最老一版的 identicalToPrevious 是 true/false 还是"未知"。
	rows, err := s.versions.ListByPipelineID(pipelineID, limit+1)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return []VersionSummary{}, nil
	}

	out := make([]VersionSummary, 0, min(len(rows), limit))
	for i, r := range rows {
		if i >= limit {
			break
		}
		snap, derr := models.DecodeSnapshot(r.Snapshot)
		if derr != nil {
			return nil, derr
		}
		item := VersionSummary{
			Version:   r.Version,
			CreatedBy: r.CreatedBy,
			CreatedAt: r.CreatedAt,
			Stages:    len(snap.Stages),
			IsCurrent: i == 0,
		}
		tasks := 0
		for _, st := range snap.Stages {
			tasks += len(st.Tasks)
		}
		item.Tasks = tasks
		if i+1 < len(rows) {
			same := string(rows[i+1].Snapshot) == string(r.Snapshot)
			item.IdenticalToPrevious = &same
		}
		out = append(out, item)
	}
	return out, nil
}

// Get returns one snapshot addressed by version number.
func (s *PipelineVersionService) Get(pipelineID uuid.UUID, version int) (*models.PipelineVersion, error) {
	if version <= 0 {
		return nil, errBadVersion
	}
	v, err := s.versions.GetByVersion(pipelineID, version)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errVersionNotFound
		}
		return nil, err
	}
	return v, nil
}

// Compare diffs two versions of a pipeline. from/to are version numbers; the
// higher one is not required to be `to` — a reverse comparison is a legitimate
// question and the diff is directional (added/removed swap).
func (s *PipelineVersionService) Compare(pipelineID uuid.UUID, from, to int) (*models.SnapshotDiff, error) {
	fv, err := s.Get(pipelineID, from)
	if err != nil {
		return nil, err
	}
	tv, err := s.Get(pipelineID, to)
	if err != nil {
		return nil, err
	}
	fsnap, err := models.DecodeSnapshot(fv.Snapshot)
	if err != nil {
		return nil, err
	}
	tsnap, err := models.DecodeSnapshot(tv.Snapshot)
	if err != nil {
		return nil, err
	}
	d := models.DiffSnapshots(fsnap, tsnap, fv.Version, tv.Version, fv.CreatedAt, tv.CreatedAt)
	return &d, nil
}

// RollbackResult reports what a rollback re-materialized and which version
// number recorded the result.
type RollbackResult struct {
	PipelineID uuid.UUID `json:"pipelineId"`
	// RestoredVersion is the snapshot that was re-materialized.
	RestoredVersion int `json:"restoredVersion"`
	// NewVersion is the freshly published version recording the rollback —
	// rollback is append-only: the old rows are never rewritten or deleted.
	NewVersion int `json:"newVersion"`
	Stages     int `json:"stages"`
	Tasks      int `json:"tasks"`
}

// Rollback re-materializes a past version's stages + task templates onto the
// live pipeline, then publishes the result as a NEW version.
//
// 语义边界（刻意）：
//   - **只回滚结构**（stages / tasks）。元信息（name / kind / description）不动 ——
//     流水线重名受 partial unique index 约束，若旧版本的 name 已被另一条活流水线
//     占用，回滚元信息会直接撞唯一键；"改结构"是本功能的明确诉求，"顺带改名字"
//     不是。
//   - **历史只追加**：不删任何 pipeline_versions 行、不重写版本号。回滚记录自己
//     也是一版，因此可从回滚再回滚。
//   - **不是事务**：materialize 分多步（先软删现有阶段、再逐个建）。中途失败会留下
//     半个结构，此时重跑同一版本即可收敛（目标快照不可变）；这是从已知状态恢复，
//     不是猜测。跨步骤原子化需要把整条链包进一个 DB 事务，属后续项。
func (s *PipelineVersionService) Rollback(pipelineID uuid.UUID, version int, createdBy string) (*RollbackResult, error) {
	target, err := s.Get(pipelineID, version)
	if err != nil {
		return nil, err
	}
	snap, err := models.DecodeSnapshot(target.Snapshot)
	if err != nil {
		return nil, err
	}
	if _, err := s.pipelines.GetByID(pipelineID); err != nil {
		return nil, errPipelineNotFound
	}

	// 先清空、后重建：新旧阶段的 sequence 往往相同，而
	// idx_stages_pipeline_seq_active 是 `where deleted_at is null` 的 partial
	// unique index —— 不先软删旧行，建同序号的阶段就会撞唯一键。
	if err := s.clearStructure(pipelineID); err != nil {
		return nil, err
	}

	res := &RollbackResult{PipelineID: pipelineID, RestoredVersion: version}
	for _, st := range snap.Stages {
		stage := &models.PipelineStage{
			PipelineID:    pipelineID,
			Name:          st.Name,
			Sequence:      st.Sequence,
			ExecutionMode: st.ExecutionMode,
		}
		if stage.ExecutionMode == "" {
			stage.ExecutionMode = models.ExecutionModeParallel
		}
		if err := s.stages.Create(stage); err != nil {
			return nil, fmt.Errorf("re-materialize stage %q: %w", st.Name, err)
		}
		res.Stages++
		for _, t := range st.Tasks {
			tpl := &models.PipelineTaskTemplate{
				StageID:        stage.ID,
				Name:           t.Name,
				Type:           t.Type,
				DisplayOrder:   t.DisplayOrder,
				Image:          t.Image,
				ScriptPath:     t.ScriptPath,
				ScriptArgs:     jsonOr(t.ScriptArgs, "[]"),
				Command:        jsonOr(t.Command, "[]"),
				Args:           jsonOr(t.Args, "[]"),
				Produces:       jsonOr(t.Produces, "[]"),
				Consumes:       jsonOr(t.Consumes, "[]"),
				RetryPolicy:    jsonOr(t.RetryPolicy, `{"maxRetries":0}`),
				TimeoutSeconds: t.TimeoutSeconds,
			}
			// 可空 JSON 列（release/rollout/approval）保持"原来没有就仍为空"——
			// 给它们补一个 '{}' 会把"未配置"伪装成"已配置"。
			if t.ReleaseConfig != nil {
				tpl.ReleaseConfig = datatypes.JSON(t.ReleaseConfig)
			}
			if t.RolloutConfig != nil {
				tpl.RolloutConfig = datatypes.JSON(t.RolloutConfig)
			}
			if t.ApprovalConfig != nil {
				tpl.ApprovalConfig = datatypes.JSON(t.ApprovalConfig)
			}
			if err := s.tasks.Create(tpl); err != nil {
				return nil, fmt.Errorf("re-materialize task %q in stage %q: %w", t.Name, st.Name, err)
			}
			res.Tasks++
		}
	}

	// 记一版新的：回滚本身也是定义变更，必须可回溯（否则历史里会缺一格）。
	if err := s.Publish(pipelineID, createdBy); err != nil {
		return nil, err
	}
	if p, perr := s.pipelines.GetByID(pipelineID); perr == nil {
		res.NewVersion = p.Version
	}
	return res, nil
}

// clearStructure soft-deletes every live stage of the pipeline; the task
// templates under each stage go with it via the service-level cascade (the DDL
// `on delete cascade` no longer fires once both sides are soft-deleted).
func (s *PipelineVersionService) clearStructure(pipelineID uuid.UUID) error {
	stages, err := s.stages.ListByPipelineID(pipelineID)
	if err != nil {
		return err
	}
	for _, st := range stages {
		if err := s.tasks.DeleteByStageID(st.ID); err != nil {
			return err
		}
		if err := s.stages.Delete(st.ID); err != nil {
			return err
		}
	}
	return nil
}

func jsonOr(raw json.RawMessage, def string) datatypes.JSON {
	if len(raw) == 0 || string(raw) == "null" {
		return datatypes.JSON(def)
	}
	return datatypes.JSON(raw)
}
