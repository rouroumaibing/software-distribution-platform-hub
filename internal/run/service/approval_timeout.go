package service

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"

	runnerapi "github.com/rouroumaibing/software-distribution-platform-runner/api/v1alpha1"

	applog "github.com/rouroumaibing/software-distribution-platform-hub/internal/common/logger"
	"github.com/rouroumaibing/software-distribution-platform-hub/internal/gateway"
	"github.com/rouroumaibing/software-distribution-platform-hub/internal/run/models"
)

// AutoTimeoutApprover is the identity recorded on approvals the sweep closed.
// 用一个**可识别的系统主体**而不是"某个管理员"：审计里必须能区分"人批的"与
// "超时自动关的"，否则超时会被误读成人做过决定。
const AutoTimeoutApprover = "system:approval-timeout"

// DefaultPendingApprovalScan bounds one sweep's scan so a backlog of pending
// approvals cannot turn the job into an unbounded query.
const DefaultPendingApprovalScan = 500

// ApprovalExpired is the **pure** timeout predicate (B-11 审批超时 / Epic 8).
//
// 语义刻意收窄：
//   - timeoutSeconds <= 0 表示"无限期等待"（runnerapi.ApprovalConfig 的契约就是
//     这样写的），**不**视为立即超时 —— 否则未配置超时的老流水线会被整批关掉；
//   - 边界取严格大于：恰好等于时限的那一刻不算超时，避免因时钟抖动在临界点反复。
func ApprovalExpired(createdAt time.Time, timeoutSeconds int, now time.Time) bool {
	if timeoutSeconds <= 0 {
		return false
	}
	return now.After(createdAt.Add(time.Duration(timeoutSeconds) * time.Second))
}

// HasApprovalGate reports whether a materialized spec contains at least one
// Approval task. Pure — 生产强审批（B-11）与审批超时共用这一个判定。
func HasApprovalGate(tasks []runnerapi.PipelineTaskSpec) bool {
	for _, t := range tasks {
		if t.Type == runnerapi.TaskTypeApproval {
			return true
		}
	}
	return false
}

// approvalTimeoutFor resolves the timeout declared by the approval task that
// taskRunID belongs to. It walks run → pipeline → stages → templates because
// the value lives on the **template** (approval_config.timeoutSeconds) while
// the pending decision lives on the run's task row.
//
// 返回 0 = 该任务没配超时（无限期等待），调用方据此跳过。
func (s *PipelineRunService) approvalTimeoutFor(run *models.PipelineRun, taskRunID uuid.UUID) (string, int, error) {
	tasks, err := s.taskRepo.ListByPipelineRunID(run.ID)
	if err != nil {
		return "", 0, err
	}
	var taskName string
	for _, tr := range tasks {
		if tr.ID == taskRunID {
			taskName = tr.TaskName
			break
		}
	}
	if taskName == "" {
		// 任务行不在（数据被清理 / 记录不完整）：没有可判定的时限，跳过。
		return "", 0, nil
	}

	stages, err := s.stageRepo.ListByPipelineID(run.PipelineID)
	if err != nil {
		return taskName, 0, err
	}
	for _, st := range stages {
		templates, terr := s.taskTemplateRepo.ListByStageID(st.ID)
		if terr != nil {
			return taskName, 0, terr
		}
		for _, tpl := range templates {
			if tpl.Name != taskName {
				continue
			}
			if len(tpl.ApprovalConfig) == 0 {
				return taskName, 0, nil
			}
			var ac runnerapi.ApprovalConfig
			if jerr := json.Unmarshal(tpl.ApprovalConfig, &ac); jerr != nil {
				// 配置坏了不等于"没配超时"：返回错误让调用方记日志并跳过，
				// 而不是静默按 0 处理（那会让一条损坏的配置永久挂着）。
				return taskName, 0, jerr
			}
			return taskName, int(ac.TimeoutSeconds), nil
		}
	}
	return taskName, 0, nil
}

// SweepApprovalTimeouts auto-closes approvals whose task declared a timeout and
// whose age exceeds it, returning how many were closed.
//
// 关单顺序与理由：
//  1. **先请求 runner 侧拒绝**（复用既有的 ApproveTask 下发），再落 hub 的审批
//     记录。runner 仍是 DAG 停顿的持有者，只有它收到拒绝并推进/失败，两侧状态
//     才一致；反过来先改 hub 记录会在下发失败时留下"hub 已超时、runner 还停着"
//     的分裂状态。
//  2. 下发失败（target 离线）时**保持 Pending** 并记日志，下一轮重试。不静默改成
//     已关闭 —— 那是在对审计撒谎。
//  3. 不在这里直接改 `pipeline_runs.phase`：运行相位由 runner 的 MessageStatusUpdate
//     回流后经 ApplyStatus 落库，hub 抢先写会与之互相覆盖。
func (s *PipelineRunService) SweepApprovalTimeouts(ctx context.Context, now time.Time) (int, error) {
	if s.approvalRepo == nil {
		return 0, nil
	}
	pending, err := s.approvalRepo.ListPending(DefaultPendingApprovalScan)
	if err != nil {
		return 0, err
	}
	closed := 0
	for _, pa := range pending {
		if pa.TaskRunID == nil {
			continue
		}
		run, rerr := s.repo.GetByID(pa.RunID)
		if rerr != nil {
			applog.Warnf("approval-timeout: run %s not found for approval %s: %v", pa.RunID, pa.ID, rerr)
			continue
		}
		taskName, timeoutSeconds, terr := s.approvalTimeoutFor(run, *pa.TaskRunID)
		if terr != nil {
			applog.Warnf("approval-timeout: cannot resolve timeout for run %s task %q: %v", run.ID, taskName, terr)
			continue
		}
		if taskName == "" || !ApprovalExpired(pa.CreatedAt, timeoutSeconds, now) {
			continue
		}

		payload := &runnerapi.ApproveTaskPayload{
			PipelineRunName: run.CRName,
			TaskName:        taskName,
			Approver:        AutoTimeoutApprover,
			Rejected:        true,
		}
		if derr := s.dispatcher.Approve(ctx, run.TargetID, payload); derr != nil {
			if errors.Is(derr, gateway.ErrNoRunner) {
				applog.Warnf("approval-timeout: target %s offline, deferring auto-reject for run %s task %q",
					run.TargetID, run.ID, taskName)
			} else {
				applog.Warnf("approval-timeout: dispatch failed for run %s task %q: %v", run.ID, taskName, derr)
			}
			continue
		}

		decidedAt := now
		pa.Status = "Cancelled"
		pa.Approver = AutoTimeoutApprover
		pa.DecidedAt = &decidedAt
		pa.DecisionComment = "auto-closed: approval timed out without a decision"
		if uerr := s.approvalRepo.Update(&pa); uerr != nil {
			applog.Warnf("approval-timeout: failed to record timeout for approval %s (runner already rejected): %v", pa.ID, uerr)
			continue
		}
		closed++
		applog.Infof("approval-timeout: auto-rejected run %s task %q (timeout=%ds, age=%s)",
			run.ID, taskName, timeoutSeconds, now.Sub(pa.CreatedAt).Truncate(time.Second))
	}
	return closed, nil
}

// RunApprovalTimeouts sweeps every interval until ctx is cancelled. A
// zero/negative interval disables the loop (returns at once).
//
// 与回收作业不同，这里**先等一个周期再扫**：启动瞬间就扫一遍没有收益（刚启动时
// 的挂起审批本来就是上一进程留下的），却会把启动路径拖长。
func (s *PipelineRunService) RunApprovalTimeouts(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		return
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if _, err := s.SweepApprovalTimeouts(ctx, time.Now()); err != nil {
				applog.Warnf("approval-timeout: sweep failed: %v", err)
			}
		}
	}
}
