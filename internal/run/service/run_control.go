package service

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"

	runnerapi "github.com/rouroumaibing/software-distribution-platform-hub/internal/runnerproto"

	"github.com/rouroumaibing/software-distribution-platform-hub/internal/common"
	applog "github.com/rouroumaibing/software-distribution-platform-hub/internal/common/logger"
	"github.com/rouroumaibing/software-distribution-platform-hub/internal/gateway"
)

// ErrRunTerminal is returned when an operator asks to cancel a run that has
// already reached a terminal phase. It is a coded *APIError so the HTTP layer
// maps it to 409 (returned unwrapped — toAPIError type-asserts, it does not
// unwrap), keeping a finished outcome from ever being rewritten.
var ErrRunTerminal = common.DomainErrorWithReasons(
	common.KindRun, http.StatusConflict, 1,
	"Run has already finished",
	"only a Pending / PendingQueue / Running / WaitingApproval run can be cancelled",
)

// CancelRun asks the Runner owning the run to stop it: mark the PipelineRun
// Cancelled and tear down its in-flight TaskRuns so no further work runs. The
// Hub records the new phase when the Runner streams the resulting status
// update back (ApplyStatus) — this method only brokers the command, exactly
// like Approve / ControlRollout.
func (s *PipelineRunService) CancelRun(ctx context.Context, runID uuid.UUID, operator string) error {
	run, err := s.repo.GetByID(runID)
	if err != nil {
		return fmt.Errorf("run %s not found: %w", runID, err)
	}

	// O-41：排队 run 尚未派发、无 runner 侧在途工作，取消为 hub 侧本地终态
	// —— 置 Cancelled + 退役 held dispatch job（否则 run 卡队列或唤醒后
	// 仍被投递）。
	if run.Phase == runnerapi.PipelineRunPendingQueue {
		run.Phase = runnerapi.PipelineRunCancelled
		now := time.Now()
		run.CompletionTime = &now
		run.Message = "cancelled while queued"
		if err := s.repo.Update(run); err != nil {
			return err
		}
		if err := s.dispatchRepo.MarkDead(run.ID, "cancelled while queued"); err != nil {
			applog.Warnf("run: retire held job of queued-cancelled run %s: %v", runID, err)
		}
		applog.Infof("run: queued run %s cancelled by %s (hub-side)", runID, operator)
		return nil
	}

	switch run.Phase {
	case runnerapi.PipelineRunPending,
		runnerapi.PipelineRunRunning,
		runnerapi.PipelineRunWaitingApproval:
		// cancellable
	default:
		return ErrRunTerminal
	}

	payload := &runnerapi.CancelPipelineRunPayload{
		PipelineRunName: run.CRName,
		Namespace:       run.CRNamespace,
		Operator:        operator,
	}
	if err := s.dispatcher.CancelPipelineRun(ctx, run.TargetID, payload); err != nil {
		if errors.Is(err, gateway.ErrNoRunner) {
			return fmt.Errorf("target for this run is offline, cannot deliver cancel: %w", err)
		}
		return err
	}
	applog.Infof("run: cancel dispatched for run %s by %s", runID, operator)
	return nil
}

// RerunTask asks the Runner owning the run to re-run a single task (and, on
// the Runner, every downstream task that depends on it) without re-dispatching
// the whole PipelineRun — the surgical counterpart to Redispatch. The Runner
// half (MessageRerunTask + RerunHandler) was already in place; this wires the
// Hub side so an operator can actually trigger it (backlog C-07).
func (s *PipelineRunService) RerunTask(ctx context.Context, runID uuid.UUID, taskName, operator string) error {
	if taskName == "" {
		return fmt.Errorf("rerun needs a task name")
	}
	run, err := s.repo.GetByID(runID)
	if err != nil {
		return fmt.Errorf("run %s not found: %w", runID, err)
	}
	// Validates the task actually belongs to this run (mirrors the
	// precondition check ControlRollout does for release tasks).
	if _, err := s.taskRepo.GetByRunAndTaskName(runID, taskName); err != nil {
		return fmt.Errorf("task %q is not part of run %s: %w", taskName, runID, err)
	}

	payload := &runnerapi.RerunTaskPayload{
		PipelineRunName: run.CRName,
		TaskName:        taskName,
		Operator:        operator,
	}
	if err := s.dispatcher.RerunTask(ctx, run.TargetID, payload); err != nil {
		if errors.Is(err, gateway.ErrNoRunner) {
			return fmt.Errorf("target for this run is offline, cannot deliver rerun: %w", err)
		}
		return err
	}
	applog.Infof("run: task rerun dispatched for run %s task %q by %s", runID, taskName, operator)
	return nil
}
