package service

import (
	"context"
	"fmt"
	"net/http"

	"github.com/google/uuid"

	"github.com/rouroumaibing/software-distribution-platform-hub/internal/common"
	applog "github.com/rouroumaibing/software-distribution-platform-hub/internal/common/logger"
	pipelinemodels "github.com/rouroumaibing/software-distribution-platform-hub/internal/pipeline/models"
	"github.com/rouroumaibing/software-distribution-platform-hub/internal/run/models"
	runnerapi "github.com/rouroumaibing/software-distribution-platform-hub/internal/runnerproto"
)

// activeRunPhases 是 O-41 的容量口径：占用同 pipeline 并发配额的非终态阶段。
// PendingQueue 自身不计——排队 run 不占坑，否则前序 run 终态后仍无空位可唤醒。
var activeRunPhases = []string{
	string(runnerapi.PipelineRunPending),
	string(runnerapi.PipelineRunRunning),
	string(runnerapi.PipelineRunWaitingApproval),
}

// DefaultRunConcurrencyLimit 是 O-41 排队口径的同 pipeline 最大活跃 run 数
// （上限 N），可经 HUB_RUN_CONCURRENCY_LIMIT 覆盖（main 装配层）。
const DefaultRunConcurrencyLimit = 4

// SetRunConcurrencyLimit 覆盖 O-41 的上限 N；<=0 视为未配置，回落默认值。
func (s *PipelineRunService) SetRunConcurrencyLimit(n int) { s.runConcurrencyLimit = n }

func (s *PipelineRunService) concurrencyLimit() int {
	if s.runConcurrencyLimit > 0 {
		return s.runConcurrencyLimit
	}
	return DefaultRunConcurrencyLimit
}

// concurrencyPolicyOf resolves the pipeline's O-41 policy. A lookup failure
// propagates: the trigger fails loudly rather than silently bypassing the
// concurrency contract.
func (s *PipelineRunService) concurrencyPolicyOf(pipelineID uuid.UUID) (string, error) {
	p, err := s.pipelineRepo.GetByID(pipelineID)
	if err != nil {
		return "", fmt.Errorf("resolve pipeline %s concurrency policy: %w", pipelineID, err)
	}
	return p.ConcurrencyPolicy, nil
}

// guardConcurrency enforces the forbid branch of O-41: any capacity-occupying
// run rejects a new trigger with 409 before anything is persisted. allow/queue
// pass through — queue defers to shouldQueue at run assembly time.
func (s *PipelineRunService) guardConcurrency(pipelineID uuid.UUID, policy string) error {
	if policy != pipelinemodels.ConcurrencyForbid {
		return nil
	}
	n, err := s.repo.CountActiveByPipeline(pipelineID, activeRunPhases)
	if err != nil {
		return fmt.Errorf("count active runs for pipeline %s: %w", pipelineID, err)
	}
	if n >= 1 {
		return common.DomainErrorWithReasons(common.KindRun, http.StatusConflict, 2,
			"Pipeline Run Concurrency Forbidden",
			fmt.Sprintf("pipeline %s has %d active run(s); concurrency policy 'forbid' rejects new triggers until they finish", pipelineID, n))
	}
	return nil
}

// shouldQueue reports whether a new run of the pipeline must land as
// PendingQueue (O-41): active runs already fill the concurrency limit N.
func (s *PipelineRunService) shouldQueue(pipelineID uuid.UUID) (bool, error) {
	n, err := s.repo.CountActiveByPipeline(pipelineID, activeRunPhases)
	if err != nil {
		return false, fmt.Errorf("count active runs for pipeline %s: %w", pipelineID, err)
	}
	return n >= int64(s.concurrencyLimit()), nil
}

// dispatchStateForRun derives the initial dispatch-job state for a run (O-41):
// a PendingQueue run gets a held job so SweepPending / DrainTarget skip it
// until promotion flips it back to pending. Lookup failure falls back to
// pending (the legacy path) rather than silently stranding a live run.
func (s *PipelineRunService) dispatchStateForRun(runID uuid.UUID) models.DispatchJobState {
	if s == nil || s.repo == nil {
		return models.DispatchJobPending
	}
	run, err := s.repo.GetByID(runID)
	if err != nil || run.Phase != runnerapi.PipelineRunPendingQueue {
		return models.DispatchJobPending
	}
	return models.DispatchJobHeld
}

// SweepQueued promotes PendingQueue runs (O-41) as capacity frees: per
// pipeline, fill up to the concurrency limit N from the oldest queued runs.
// Promotion flips the run back to Pending and releases its held dispatch job
// so the normal delivery machinery (DrainTarget / SweepPending) takes over
// unchanged. Per-run failures are logged and swallowed — housekeeping must
// never take the trigger path down.
func (s *PipelineRunService) SweepQueued(_ context.Context) error {
	queued, err := s.repo.FindQueued(1000)
	if err != nil {
		return err
	}
	byPipeline := map[uuid.UUID][]models.PipelineRun{}
	for _, r := range queued {
		byPipeline[r.PipelineID] = append(byPipeline[r.PipelineID], r)
	}
	for pid, runs := range byPipeline {
		n, err := s.repo.CountActiveByPipeline(pid, activeRunPhases)
		if err != nil {
			applog.Warnf("queued-run sweep: count active pipeline=%s: %v", pid, err)
			continue
		}
		capacity := int64(s.concurrencyLimit()) - n
		for i := 0; i < len(runs) && capacity > 0; i++ {
			run := runs[i]
			run.Phase = runnerapi.PipelineRunPending
			if err := s.repo.Update(&run); err != nil {
				applog.Warnf("queued-run sweep: promote run=%s: %v", run.ID, err)
				continue
			}
			if _, err := s.dispatchRepo.ReleaseHeld(run.ID); err != nil {
				applog.Warnf("queued-run sweep: release held job run=%s: %v", run.ID, err)
				continue
			}
			capacity--
			applog.Infof("queued-run sweep: promoted run=%s pipeline=%s", run.ID, pid)
		}
	}
	return nil
}
