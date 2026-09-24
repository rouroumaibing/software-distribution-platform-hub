package service

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/google/uuid"

	runnerapi "github.com/rouroumaibing/software-distribution-platform-runner/api/v1alpha1"

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
	"only a Pending / Running / WaitingApproval run can be cancelled",
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
