package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"
	"gorm.io/gorm"

	runnerapi "github.com/rouroumaibing/software-distribution-platform-runner/api/v1alpha1"

	clustermodels "github.com/rouroumaibing/software-distribution-platform-hub/internal/cluster/models"
	"github.com/rouroumaibing/software-distribution-platform-hub/internal/common"
	applog "github.com/rouroumaibing/software-distribution-platform-hub/internal/common/logger"
	pipelinemodels "github.com/rouroumaibing/software-distribution-platform-hub/internal/pipeline/models"
	"github.com/rouroumaibing/software-distribution-platform-hub/internal/run/models"

	"github.com/rouroumaibing/software-distribution-platform-hub/internal/gateway"
)

// ErrNoOnlineCluster is returned by Trigger when no Runner is connected and
// reachable to accept the dispatched work.
var ErrNoOnlineCluster = errors.New("no online cluster available to run this pipeline")

// Dispatcher is the narrow interface the run service needs to push work to a
// Runner. The gateway implements it; declaring it here keeps the service
// decoupled from the gateway package.
type Dispatcher interface {
	Dispatch(ctx context.Context, clusterID uuid.UUID, payload *runnerapi.ApplyPipelineRunPayload) error
	// Approve relays an approver's decision for a paused Approval task to the
	// Runner owning the run's cluster.
	Approve(ctx context.Context, clusterID uuid.UUID, payload *runnerapi.ApproveTaskPayload) error
	// RolloutControl relays an operator's pause/promote/rollback command for
	// a Release task's Rollout to the Runner owning the run's cluster.
	RolloutControl(ctx context.Context, clusterID uuid.UUID, payload *runnerapi.RolloutControlPayload) error
}

// DispatchJobStore is the narrow persistence surface the run service needs for
// the durable dispatch queue. Declared as an interface (rather than depending
// on the concrete *repository.DispatchJobRepository) so callers and tests can
// substitute a fake without a live Postgres.
type DispatchJobStore interface {
	Create(*models.DispatchJob) error
	GetByID(id uuid.UUID) (*models.DispatchJob, error)
	ListPendingByCluster(clusterID uuid.UUID) ([]models.DispatchJob, error)
	MarkDispatching(id uuid.UUID) (int64, error)
	MarkDispatched(id uuid.UUID) error
	MarkFailed(id uuid.UUID, lastErr string, nextRetryAt time.Time) error
	MarkDead(id uuid.UUID, lastErr string) error
	ListDueRetry(now time.Time) ([]models.DispatchJob, error)
	// LatestByRun returns the most recently created dispatch job for a run,
	// used by Redispatch to copy the last payload for a retry.
	LatestByRun(pipelineRunID uuid.UUID) (*models.DispatchJob, error)
}

// PipelineRunStore is the persistence surface over pipeline_runs. Declared as
// an interface so tests can substitute an in-memory fake without Postgres.
type PipelineRunStore interface {
	Create(*models.PipelineRun) error
	GetByID(id uuid.UUID) (*models.PipelineRun, error)
	Update(*models.PipelineRun) error
	GetByCRNameCluster(crName string, clusterID uuid.UUID) (*models.PipelineRun, error)
	FindByPipelineID(pipelineID uuid.UUID, p common.Pagination) ([]models.PipelineRun, int64, error)
	FindAll(p common.Pagination, phase string) ([]models.PipelineRun, int64, error)
}

// TaskRunStore is the persistence surface over task_runs.
type TaskRunStore interface {
	Upsert(*models.TaskRun) error
	SaveStatus(runID uuid.UUID, ts runnerapi.TaskRunStatusSummary) error
	GetByRunAndTaskName(runID uuid.UUID, taskName string) (*models.TaskRun, error)
	ListByPipelineRunID(runID uuid.UUID) ([]models.TaskRun, error)
}

// PipelineDefStore reads the pipeline definition (version + component).
type PipelineDefStore interface {
	GetByID(id uuid.UUID) (*pipelinemodels.Pipeline, error)
}

// StageStore lists a pipeline's stages in sequence order.
type StageStore interface {
	ListByPipelineID(pipelineID uuid.UUID) ([]pipelinemodels.PipelineStage, error)
}

// TaskTemplateStore lists a stage's task templates.
type TaskTemplateStore interface {
	ListByStageID(stageID uuid.UUID) ([]pipelinemodels.PipelineTaskTemplate, error)
}

// ClusterStore resolves target clusters by id / lists online ones.
type ClusterStore interface {
	Get(id uuid.UUID) (*clustermodels.Cluster, error)
	List(p common.Pagination) ([]clustermodels.Cluster, int64, error)
}

// LogStore persists streamed log chunks for later retrieval by the G2
// log-read endpoint. Declared as an interface so tests can substitute a fake
// without a live Postgres.
type LogStore interface {
	Append(*models.TaskRunLog) error
	ListByRunTask(runID uuid.UUID, taskName string, p common.Pagination) ([]models.TaskRunLog, int64, error)
}

// PipelineRunService intentionally does NOT satisfy common.CRUDService —
// a run isn't created from arbitrary user JSON (it's assembled server-side
// from a Pipeline's current stages/tasks + target cluster), and Delete
// isn't a normal operation on history. Handler wires these methods by hand.
type PipelineRunService struct {
	repo             PipelineRunStore
	taskRepo         TaskRunStore
	pipelineRepo     PipelineDefStore
	stageRepo        StageStore
	taskTemplateRepo TaskTemplateStore
	clusterSvc       ClusterStore
	dispatcher       Dispatcher
	dispatchRepo     DispatchJobStore
	logRepo          LogStore

	// sweepInterval controls how often SweepPending retries failed dispatch
	// jobs. Overridable in tests; defaults to 15s.
	sweepInterval time.Duration
}

func NewPipelineRunService(
	repo PipelineRunStore,
	taskRepo TaskRunStore,
	pipelineRepo PipelineDefStore,
	stageRepo StageStore,
	taskTemplateRepo TaskTemplateStore,
	clusterSvc ClusterStore,
	dispatcher Dispatcher,
	dispatchRepo DispatchJobStore,
	logRepo LogStore,
) *PipelineRunService {
	return &PipelineRunService{
		repo:             repo,
		taskRepo:         taskRepo,
		pipelineRepo:     pipelineRepo,
		stageRepo:        stageRepo,
		taskTemplateRepo: taskTemplateRepo,
		clusterSvc:       clusterSvc,
		dispatcher:       dispatcher,
		dispatchRepo:     dispatchRepo,
		logRepo:          logRepo,
		sweepInterval:    15 * time.Second,
	}
}

// Trigger assembles a PipelineRunSpec from the pipeline's current stages/tasks
// and dispatches it. If req.TargetClusters is set, the same trigger fans out to
// every listed environment (one independent PipelineRun per cluster); otherwise
// a single run is created on the auto/selected cluster.
func (s *PipelineRunService) Trigger(pipelineID uuid.UUID, req *models.TriggerRequest) ([]*models.PipelineRun, error) {
	if len(req.TargetClusters) > 0 {
		return s.triggerFanout(pipelineID, req)
	}

	clusterID, err := s.selectCluster(req.ClusterID)
	if err != nil {
		return nil, err
	}
	spec, err := s.buildSpec(pipelineID, req)
	if err != nil {
		return nil, err
	}
	p, err := s.pipelineRepo.GetByID(pipelineID)
	if err != nil {
		return nil, err
	}
	ns := req.TargetNamespace
	if ns == "" {
		ns = "sdp-run"
	}
	paramsJSON, err := json.Marshal(req.Params)
	if err != nil {
		return nil, err
	}
	run, err := s.createRun(pipelineID, clusterID, spec, ns, paramsJSON, p.Version, req)
	if err != nil {
		return nil, err
	}
	return []*models.PipelineRun{run}, nil
}

// triggerFanout creates one independent PipelineRun per target environment.
// Each environment keeps the 1:1 run↔cluster invariant, so all existing status
// / TaskRun logic works unchanged. Target clusters only need to EXIST (not be
// online): the durable dispatch queue delivers each run once its Runner
// reconnects, so a momentarily-offline environment never fails the trigger.
func (s *PipelineRunService) triggerFanout(pipelineID uuid.UUID, req *models.TriggerRequest) ([]*models.PipelineRun, error) {
	for _, cid := range req.TargetClusters {
		if _, err := s.clusterSvc.Get(cid); err != nil {
			return nil, fmt.Errorf("target cluster %s not found: %w", cid, err)
		}
	}

	spec, err := s.buildSpec(pipelineID, req)
	if err != nil {
		return nil, err
	}
	p, err := s.pipelineRepo.GetByID(pipelineID)
	if err != nil {
		return nil, err
	}
	ns := req.TargetNamespace
	if ns == "" {
		ns = "sdp-run"
	}
	paramsJSON, err := json.Marshal(req.Params)
	if err != nil {
		return nil, err
	}

	runs := make([]*models.PipelineRun, 0, len(req.TargetClusters))
	for _, cid := range req.TargetClusters {
		run, err := s.createRun(pipelineID, cid, spec, ns, paramsJSON, p.Version, req)
		if err != nil {
			return nil, err
		}
		runs = append(runs, run)
	}
	return runs, nil
}

// createRun persists one PipelineRun + its seeded TaskRun rows, then enqueues a
// durable dispatch job. A failed delivery (Runner offline) does NOT fail the
// run — it stays Pending and the job is redelivered later by DrainCluster /
// SweepPending. Only a failure to persist the run/job itself is fatal.
func (s *PipelineRunService) createRun(pipelineID, clusterID uuid.UUID, spec *runnerapi.PipelineRunSpec, ns string, paramsJSON []byte, version int, req *models.TriggerRequest) (*models.PipelineRun, error) {
	run := &models.PipelineRun{
		PipelineID:      pipelineID,
		ClusterID:       clusterID,
		CRName:          fmt.Sprintf("pr-%s", uuid.New().String()[:8]),
		CRNamespace:     ns,
		CommitSHA:       req.CommitSHA,
		Params:          datatypes.JSON(paramsJSON),
		PipelineVersion: version,
		Phase:           runnerapi.PipelineRunPending,
		TriggeredBy:     req.TriggeredBy,
	}
	if err := s.repo.Create(run); err != nil {
		return nil, err
	}

	// Seed a Pending TaskRun row per DAG node so the UI has something to
	// show before the first status update arrives.
	for _, t := range spec.Tasks {
		tr := &models.TaskRun{
			PipelineRunID: run.ID,
			TaskName:      t.Name,
			StageName:     t.Stage,
			Type:          t.Type,
			Phase:         runnerapi.TaskRunPending,
		}
		if err := s.taskRepo.Upsert(tr); err != nil {
			return nil, err
		}
	}

	// Wrap the resolved spec in the dispatch envelope carrying the CR
	// name/namespace the Runner must use, so the status it streams back
	// routes to this exact run row (ApplyStatus looks up by CRName+cluster).
	apply := &runnerapi.ApplyPipelineRunPayload{
		Name:      run.CRName,
		Namespace: run.CRNamespace,
		Spec:      *spec,
	}
	// Enqueue a durable dispatch job instead of dispatching inline. This
	// decouples run creation from Runner connectivity: if the Runner is
	// momentarily unreachable (or reconnects later), the pending job is
	// redelivered by DrainCluster (on reconnect) and SweepPending (on
	// backoff), and the run stays Pending rather than failing the trigger.
	if err := s.enqueueDispatch(run.ID, clusterID, apply); err != nil {
		run.Phase = runnerapi.PipelineRunFailed
		run.Message = "failed to enqueue dispatch: " + err.Error()
		_ = s.repo.Update(run)
		return nil, err
	}
	return run, nil
}

// ApplyStatus is invoked by the gateway whenever a Runner reports a status
// update for one of our runs. It advances the run's phase and upserts each
// task's progress. Unknown runs (e.g. from a Runner that restarted) are
// ignored so a stray message can't create orphan history.
func (s *PipelineRunService) ApplyStatus(ctx context.Context, clusterID uuid.UUID, payload *runnerapi.StatusUpdatePayload) error {
	run, err := s.repo.GetByCRNameCluster(payload.PipelineRunName, clusterID)
	if err != nil {
		applog.Infof("run: status for unknown run %s on cluster %s (ignored): %v", payload.PipelineRunName, clusterID, err)
		return nil
	}

	run.Phase = payload.Phase
	run.Message = payload.Message
	if payload.StartTime != nil {
		t := payload.StartTime.Time
		run.StartTime = &t
	}
	if payload.CompletionTime != nil {
		t := payload.CompletionTime.Time
		run.CompletionTime = &t
	}
	if err := s.repo.Update(run); err != nil {
		return err
	}

	for _, ts := range payload.Tasks {
		if err := s.taskRepo.SaveStatus(run.ID, ts); err != nil {
			return err
		}
	}
	return nil
}

// Approve relays an approver's decision for a paused Approval-type task down
// to the Runner owning the run's cluster. The Runner patches the TaskRun's
// Approval status (recording Approver / RejectedBy, advancing to Succeeded
// once RequiredCount approvers sign off, or failing the run on a rejection);
// the resulting phase change is then streamed back via MessageStatusUpdate
// and persisted by ApplyStatus. The hub intentionally does not store approval
// detail itself (task_runs has no approval columns) — it only brokers the
// decision.
func (s *PipelineRunService) Approve(ctx context.Context, pipelineID, runID uuid.UUID, taskName string, approved bool, approver string) error {
	run, err := s.repo.GetByID(runID)
	if err != nil {
		return fmt.Errorf("run %s not found: %w", runID, err)
	}
	if run.PipelineID != pipelineID {
		return fmt.Errorf("run %s does not belong to pipeline %s", runID, pipelineID)
	}
	tr, err := s.taskRepo.GetByRunAndTaskName(runID, taskName)
	if err != nil {
		return fmt.Errorf("task %q is not part of run %s: %w", taskName, runID, err)
	}
	if tr.Type != runnerapi.TaskTypeApproval {
		return fmt.Errorf("task %q is not an approval task (type=%s)", taskName, tr.Type)
	}
	if tr.Phase == runnerapi.TaskRunSucceeded || tr.Phase == runnerapi.TaskRunFailed {
		return fmt.Errorf("task %q has already been decided (phase=%s)", taskName, tr.Phase)
	}
	if approver == "" {
		return fmt.Errorf("approver identity is required")
	}

	payload := &runnerapi.ApproveTaskPayload{
		PipelineRunName: run.CRName,
		TaskName:        taskName,
		Approver:        approver,
		Rejected:        !approved,
	}
	if err := s.dispatcher.Approve(ctx, run.ClusterID, payload); err != nil {
		if errors.Is(err, gateway.ErrNoRunner) {
			return fmt.Errorf("cluster for this run is offline, cannot deliver approval decision: %w", err)
		}
		return err
	}
	applog.Infof("run: approval decision dispatched for run %s task %q approved=%v by %s", runID, taskName, approved, approver)
	return nil
}

// ControlRollout relays an operator's progressive-delivery command (pause /
// promote / rollback) for a Release task's Rollout to the Runner owning the
// run's cluster. Like Approve, the hub only brokers the command — the Runner
// stamps it on the Rollout CR, its reconciler applies it, and the resulting
// phase change streams back via MessageStatusUpdate and is persisted by
// ApplyStatus.
func (s *PipelineRunService) ControlRollout(ctx context.Context, runID uuid.UUID, taskName string, action runnerapi.RolloutAction, operator string) error {
	switch action {
	case runnerapi.RolloutActionPause, runnerapi.RolloutActionPromote, runnerapi.RolloutActionRollback:
	default:
		return fmt.Errorf("unknown rollout action %q (want pause|promote|rollback)", action)
	}
	run, err := s.repo.GetByID(runID)
	if err != nil {
		return fmt.Errorf("run %s not found: %w", runID, err)
	}
	tr, err := s.taskRepo.GetByRunAndTaskName(runID, taskName)
	if err != nil {
		return fmt.Errorf("task %q is not part of run %s: %w", taskName, runID, err)
	}
	if tr.Type != runnerapi.TaskTypeRelease {
		return fmt.Errorf("task %q is not a release task (type=%s)", taskName, tr.Type)
	}

	payload := &runnerapi.RolloutControlPayload{
		PipelineRunName: run.CRName,
		TaskName:        taskName,
		Action:          action,
		Operator:        operator,
	}
	if err := s.dispatcher.RolloutControl(ctx, run.ClusterID, payload); err != nil {
		if errors.Is(err, gateway.ErrNoRunner) {
			return fmt.Errorf("cluster for this run is offline, cannot deliver rollout control: %w", err)
		}
		return err
	}
	applog.Infof("run: rollout control %s dispatched for run %s task %q by %s", action, runID, taskName, operator)
	return nil
}

func (s *PipelineRunService) Get(id uuid.UUID) (*models.PipelineRun, error) {
	return s.repo.GetByID(id)
}

func (s *PipelineRunService) ListByPipeline(pipelineID uuid.UUID, p common.Pagination) ([]models.PipelineRun, int64, error) {
	return s.repo.FindByPipelineID(pipelineID, p)
}

// ListAll lists runs across every pipeline — the Run Center's global feed.
// An empty phase means no filtering.
func (s *PipelineRunService) ListAll(p common.Pagination, phase string) ([]models.PipelineRun, int64, error) {
	return s.repo.FindAll(p, phase)
}

func (s *PipelineRunService) ListTasks(runID uuid.UUID) ([]models.TaskRun, error) {
	return s.taskRepo.ListByPipelineRunID(runID)
}

// RecordLogChunk persists a log chunk streamed from a Runner over
// MessageLogChunk. It resolves the (cluster, pipelineRunName) pair to a hub
// PipelineRun (the same lookup ApplyStatus uses), then appends the chunk to
// that run's task log. Chunks for unknown runs — e.g. a Runner that restarted
// and replays stale output — are ignored so they can't create orphan log
// rows. A run-level chunk (no task name) is stored under the RunLevelLogBucket
// so the console can show init/teardown output that isn't tied to a DAG node.
func (s *PipelineRunService) RecordLogChunk(ctx context.Context, clusterID uuid.UUID, payload *runnerapi.LogChunkPayload) error {
	run, err := s.repo.GetByCRNameCluster(payload.PipelineRunName, clusterID)
	if err != nil {
		applog.Infof("run: log for unknown run %s on cluster %s (ignored): %v", payload.PipelineRunName, clusterID, err)
		return nil
	}
	taskName := payload.TaskName
	if taskName == "" {
		taskName = models.RunLevelLogBucket
	}
	log := &models.TaskRunLog{
		PipelineRunID: run.ID,
		TaskName:      taskName,
		Stream:        payload.Stream,
		Chunk:         payload.Chunk,
	}
	return s.logRepo.Append(log)
}

// GetLogs returns the ordered log chunks for a (run, task) pair. A run-level
// request (taskName == RunLevelLogBucket) returns the chunks the Runner
// streamed without a task name. The run must exist, otherwise a not-found
// error is returned so the caller knows the run id is wrong rather than
// silently returning an empty page.
func (s *PipelineRunService) GetLogs(runID uuid.UUID, taskName string, p common.Pagination) ([]models.TaskRunLog, int64, error) {
	if _, err := s.repo.GetByID(runID); err != nil {
		return nil, 0, err
	}
	return s.logRepo.ListByRunTask(runID, taskName, p)
}

// selectCluster resolves the destination cluster: an explicit, online
// clusterId wins; otherwise the first online cluster is used. Errors if no
// Runner is currently connected.
func (s *PipelineRunService) selectCluster(reqClusterID *uuid.UUID) (uuid.UUID, error) {
	if reqClusterID != nil {
		cl, err := s.clusterSvc.Get(*reqClusterID)
		if err != nil {
			return uuid.Nil, err
		}
		if cl.Status != clustermodels.ClusterStatusOnline {
			return uuid.Nil, fmt.Errorf("cluster %q is not online", cl.Name)
		}
		return cl.ID, nil
	}
	online, _, err := s.clusterSvc.List(common.Pagination{Page: 1, PageSize: 100})
	if err != nil {
		return uuid.Nil, err
	}
	for _, cl := range online {
		if cl.Status == clustermodels.ClusterStatusOnline {
			return cl.ID, nil
		}
	}
	return uuid.Nil, ErrNoOnlineCluster
}

// buildSpec materializes the pipeline's current stages + task templates into
// a runnerapi.PipelineRunSpec, deriving DependsOn across stages per the
// platform's DAG convention (every task in a later stage depends on every
// task in the immediately preceding stage, unless hand-authored).
func (s *PipelineRunService) buildSpec(pipelineID uuid.UUID, req *models.TriggerRequest) (*runnerapi.PipelineRunSpec, error) {
	p, err := s.pipelineRepo.GetByID(pipelineID)
	if err != nil {
		return nil, err
	}
	stages, err := s.stageRepo.ListByPipelineID(pipelineID)
	if err != nil {
		return nil, err
	}

	spec := &runnerapi.PipelineRunSpec{
		PipelineRef:     p.Name,
		ProjectID:       p.ComponentID.String(),
		TargetNamespace: req.TargetNamespace,
		TriggeredBy:     req.TriggeredBy,
	}
	if spec.TargetNamespace == "" {
		spec.TargetNamespace = "sdp-run"
	}
	if req.RepoURL != "" {
		spec.Repo = &runnerapi.RepoSource{URL: req.RepoURL, Ref: req.RepoRef, Path: req.RepoPath}
	}
	if len(req.Params) > 0 {
		spec.Params = req.Params
	}

	var prevStageTaskNames []string
	for _, stage := range stages {
		templates, err := s.taskTemplateRepo.ListByStageID(stage.ID)
		if err != nil {
			return nil, err
		}
		var stageTaskNames []string
		for _, tpl := range templates {
			task, err := templateToTaskSpec(&tpl)
			if err != nil {
				return nil, err
			}
			task.Stage = stage.Name
			// Derive DependsOn from the previous stage only when the task
			// doesn't already declare its own dependencies.
			if len(task.DependsOn) == 0 && len(prevStageTaskNames) > 0 {
				task.DependsOn = append(task.DependsOn, prevStageTaskNames...)
			}
			spec.Tasks = append(spec.Tasks, *task)
			stageTaskNames = append(stageTaskNames, task.Name)
		}
		prevStageTaskNames = stageTaskNames
	}

	if len(spec.Tasks) == 0 {
		return nil, fmt.Errorf("pipeline %s has no task templates to run", pipelineID)
	}
	return spec, nil
}

// templateToTaskSpec converts a definition-level PipelineTaskTemplate into the
// runner-facing PipelineTaskSpec, decoding the JSON fields stored on the
// template (script args, produces/consumes, retry policy, and the
// type-specific ApprovalConfig / RolloutConfig).
func templateToTaskSpec(tpl *pipelinemodels.PipelineTaskTemplate) (*runnerapi.PipelineTaskSpec, error) {
	task := &runnerapi.PipelineTaskSpec{
		Name:           tpl.Name,
		Type:           tpl.Type,
		Image:          tpl.Image,
		ScriptPath:     tpl.ScriptPath,
		TimeoutSeconds: int64(tpl.TimeoutSeconds),
	}
	if err := json.Unmarshal(tpl.ScriptArgs, &task.ScriptArgs); err != nil {
		return nil, fmt.Errorf("task %q scriptArgs: %w", tpl.Name, err)
	}
	// Build tasks carry an inline command + args; falls back to ScriptPath
	// when Command is empty (script escape hatch).
	if len(tpl.Command) > 0 {
		if err := json.Unmarshal(tpl.Command, &task.Command); err != nil {
			return nil, fmt.Errorf("task %q command: %w", tpl.Name, err)
		}
	}
	if len(tpl.Args) > 0 {
		if err := json.Unmarshal(tpl.Args, &task.Args); err != nil {
			return nil, fmt.Errorf("task %q args: %w", tpl.Name, err)
		}
	}
	if err := json.Unmarshal(tpl.Produces, &task.Produces); err != nil {
		return nil, fmt.Errorf("task %q produces: %w", tpl.Name, err)
	}
	if err := json.Unmarshal(tpl.Consumes, &task.Consumes); err != nil {
		return nil, fmt.Errorf("task %q consumes: %w", tpl.Name, err)
	}
	if len(tpl.RetryPolicy) > 0 && string(tpl.RetryPolicy) != "null" {
		var rp runnerapi.RetryPolicy
		if err := json.Unmarshal(tpl.RetryPolicy, &rp); err != nil {
			return nil, fmt.Errorf("task %q retryPolicy: %w", tpl.Name, err)
		}
		task.RetryPolicy = &rp
	}
	if tpl.Type == runnerapi.TaskTypeApproval && len(tpl.ApprovalConfig) > 0 {
		var ac runnerapi.ApprovalConfig
		if err := json.Unmarshal(tpl.ApprovalConfig, &ac); err != nil {
			return nil, fmt.Errorf("task %q approvalConfig: %w", tpl.Name, err)
		}
		task.ApprovalConfig = &ac
	}
	if tpl.Type == runnerapi.TaskTypeRelease {
		// Canary (progressive rollout) is optional for Release tasks.
		if len(tpl.RolloutConfig) > 0 {
			var rs runnerapi.RolloutSpec
			if err := json.Unmarshal(tpl.RolloutConfig, &rs); err != nil {
				return nil, fmt.Errorf("task %q rolloutConfig: %w", tpl.Name, err)
			}
			task.RolloutSpec = &rs
		}
		// Chart/manifest source + values from parameter management.
		if len(tpl.ReleaseConfig) > 0 {
			var rs runnerapi.ReleaseSpec
			if err := json.Unmarshal(tpl.ReleaseConfig, &rs); err != nil {
				return nil, fmt.Errorf("task %q releaseConfig: %w", tpl.Name, err)
			}
			task.ReleaseSpec = &rs
		}
	}
	return task, nil
}

// enqueueDispatch persists a pending DispatchJob carrying the exact payload,
// then attempts immediate delivery. A delivery failure (e.g. Runner briefly
// offline) is NOT surfaced as a trigger error: the run stays Pending and the
// job is redelivered later. Only a failure to persist the job itself is fatal.
func (s *PipelineRunService) enqueueDispatch(runID, clusterID uuid.UUID, apply *runnerapi.ApplyPipelineRunPayload) error {
	payload, err := json.Marshal(apply)
	if err != nil {
		return fmt.Errorf("marshal dispatch payload: %w", err)
	}
	job := &models.DispatchJob{
		PipelineRunID: runID,
		ClusterID:     clusterID,
		Payload:       payload,
		State:         models.DispatchJobPending,
	}
	if err := s.dispatchRepo.Create(job); err != nil {
		return err
	}
	if err := s.tryDeliver(context.Background(), job.ID); err != nil {
		// Delivery will be retried; the run itself remains Pending.
		applog.Infof("run: dispatch enqueued, delivery pending for run %s cluster %s: %v", runID, clusterID, err)
	}
	return nil
}

// tryDeliver claims a dispatch job and pushes its payload to the target
// cluster's Runner. It is safe to call concurrently: MarkDispatching only
// claims the job when it is still pending/failed, so duplicate calls (e.g.
// from both a reconnect and the sweeper) collapse to a single delivery.
func (s *PipelineRunService) tryDeliver(ctx context.Context, jobID uuid.UUID) error {
	job, err := s.dispatchRepo.GetByID(jobID)
	if err != nil {
		return err
	}
	switch job.State {
	case models.DispatchJobDispatched, models.DispatchJobDead:
		return nil // already delivered or given up
	}

	affected, err := s.dispatchRepo.MarkDispatching(jobID)
	if err != nil {
		return err
	}
	if affected == 0 {
		return nil // claimed by another caller
	}

	var payload runnerapi.ApplyPipelineRunPayload
	if err := json.Unmarshal(job.Payload, &payload); err != nil {
		// Corrupt payload is unrecoverable; retire the job.
		_ = s.dispatchRepo.MarkDead(jobID, "corrupt payload: "+err.Error())
		return err
	}

	if err := s.dispatcher.Dispatch(ctx, job.ClusterID, &payload); err != nil {
		newAttempts := job.Attempts + 1
		if newAttempts >= MaxDispatchAttempts {
			_ = s.dispatchRepo.MarkDead(jobID, err.Error())
		} else {
			next := time.Now().Add(dispatchBackoff(newAttempts))
			_ = s.dispatchRepo.MarkFailed(jobID, err.Error(), next)
		}
		return err
	}

	if err := s.dispatchRepo.MarkDispatched(jobID); err != nil {
		return err
	}
	applog.Infof("run: dispatched run %s to cluster %s (job %s)", job.PipelineRunID, job.ClusterID, jobID)
	return nil
}

// DrainCluster redelivers any pending dispatch jobs for a cluster that just
// (re)connected, so work enqueued while it was offline is not lost.
func (s *PipelineRunService) DrainCluster(ctx context.Context, clusterID uuid.UUID) error {
	jobs, err := s.dispatchRepo.ListPendingByCluster(clusterID)
	if err != nil {
		return err
	}
	for i := range jobs {
		if err := s.tryDeliver(ctx, jobs[i].ID); err != nil {
			applog.Infof("run: drain delivery failed for job %s: %v", jobs[i].ID, err)
		}
	}
	return nil
}

// SweepPending retries failed dispatch jobs whose backoff has elapsed. It
// blocks until ctx is cancelled (graceful shutdown).
func (s *PipelineRunService) SweepPending(ctx context.Context) {
	ticker := time.NewTicker(s.sweepInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.sweepOnce(ctx)
		}
	}
}

func (s *PipelineRunService) sweepOnce(ctx context.Context) {
	jobs, err := s.dispatchRepo.ListDueRetry(time.Now())
	if err != nil {
		applog.Infof("run: dispatch sweep list failed: %v", err)
		return
	}
	for i := range jobs {
		if err := s.tryDeliver(ctx, jobs[i].ID); err != nil {
			applog.Infof("run: dispatch sweep delivery failed for job %s: %v", jobs[i].ID, err)
		}
	}
}

// MaxDispatchAttempts is the number of delivery attempts before a dispatch job
// is retired as dead.
const MaxDispatchAttempts = 10

// dispatchBackoff returns the wait before the next retry, growing
// exponentially from 30s and capped at 5m.
func dispatchBackoff(attempts int) time.Duration {
	const base = 30 * time.Second
	const max = 5 * time.Minute
	d := base * time.Duration(1<<uint(attempts-1))
	if d > max {
		d = max
	}
	return d
}

// Progress returns a run's current phase plus its per-task status rows, for
// the console's high-frequency progress polling. It intentionally returns
// status only (not the full pipeline definition) so repeated polls stay cheap.
func (s *PipelineRunService) Progress(runID uuid.UUID) (*models.PipelineRun, []models.TaskRun, error) {
	run, err := s.repo.GetByID(runID)
	if err != nil {
		return nil, nil, err
	}
	tasks, err := s.taskRepo.ListByPipelineRunID(runID)
	if err != nil {
		return nil, nil, err
	}
	return run, tasks, nil
}

// Redispatch re-delivers a run whose dispatch job is stuck (failed/dead) or was
// never confirmed delivered. It copies the payload of the run's most recent
// dispatch job into a fresh pending job — so a retry never re-derives a
// possibly-drifted spec — and attempts delivery. A Runner that is offline leaves
// the new job pending; it is delivered on reconnect or by the sweeper, so
// Redispatch itself always succeeds unless the run or its dispatch history is
// missing. A run stuck in Failed is reset to Pending so the console reflects the
// redelivery.
func (s *PipelineRunService) Redispatch(ctx context.Context, runID uuid.UUID) (*models.DispatchJob, error) {
	run, err := s.repo.GetByID(runID)
	if err != nil {
		return nil, err
	}

	last, err := s.dispatchRepo.LatestByRun(runID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("run %s has no dispatch record; re-trigger the pipeline instead", runID)
		}
		return nil, err
	}

	// Already in flight or confirmed delivered: nothing to redeliver.
	if last.State == models.DispatchJobDispatching || last.State == models.DispatchJobDispatched {
		return last, nil
	}

	job := &models.DispatchJob{
		PipelineRunID: runID,
		ClusterID:     last.ClusterID,
		Payload:       last.Payload,
		State:         models.DispatchJobPending,
	}
	if err := s.dispatchRepo.Create(job); err != nil {
		return nil, err
	}

	// Reset a stuck (failed) run so the console reflects the redelivery.
	if run.Phase == runnerapi.PipelineRunFailed {
		run.Phase = runnerapi.PipelineRunPending
		run.Message = "redispatch requested"
		if err := s.repo.Update(run); err != nil {
			applog.Infof("run: reset phase on redispatch failed: %v", err)
		}
	}

	// Best-effort delivery; an offline Runner leaves the job pending.
	if err := s.tryDeliver(ctx, job.ID); err != nil {
		applog.Infof("run: redispatch enqueued, delivery pending for run %s: %v", runID, err)
	}
	// Return the job's current state (dispatched / pending / failed) so the
	// caller sees the outcome of the redelivery attempt, not the pre-delivery copy.
	return s.dispatchRepo.GetByID(job.ID)
}
