package service

import (
	"context"
	"fmt"
	"time"

	runnerapi "github.com/rouroumaibing/software-distribution-platform-hub/internal/runnerproto"

	applog "github.com/rouroumaibing/software-distribution-platform-hub/internal/common/logger"
)

// SweepRunTimeouts fails runs whose O-39 run-level timeout has elapsed. A run is
// "timed out" when timeout_seconds>0 and COALESCE(start_time, created_at) +
// timeout_seconds has passed and it is still non-terminal. The terminal write
// mirrors ApplyStatus so the finished-hook / notify sink fire as for any other
// terminal transition. Failures are logged and swallowed — this is a
// housekeeping job; it must never take the hub down or retry-storm.
func (s *PipelineRunService) SweepRunTimeouts(ctx context.Context, now time.Time) (int, error) {
	runs, err := s.repo.FindTimedOut(now)
	if err != nil {
		return 0, err
	}
	n := 0
	for i := range runs {
		run := runs[i]
		run.Phase = runnerapi.PipelineRunFailed
		t := now
		run.CompletionTime = &t
		// reason 编码进 Message（无独立 reason 列）；前端据此展示"超时失败"。
		run.Message = "PipelineRunTimeout"
		if err := s.repo.Update(&run); err != nil {
			applog.Warnf("run-timeout: mark failed failed id=%s err=%v", run.ID, err)
			continue
		}
		n++
		if s.finishedHook != nil {
			func() {
				defer func() {
					if r := recover(); r != nil {
						applog.Warnf("run-timeout: finished hook panicked for run %s: %v", run.ID, r)
					}
				}()
				s.finishedHook(run.PipelineID, string(runnerapi.PipelineRunFailed))
			}()
		}
		if s.notifySink != nil && run.TriggeredBy != "" {
			func() {
				defer func() {
					if r := recover(); r != nil {
						applog.Warnf("run-timeout: notify sink panicked for run %s: %v", run.ID, r)
					}
				}()
				s.notifySink("run_failed", run.TriggeredBy,
					fmt.Sprintf("[SDP] 运行超时失败：%s", run.CRName),
					fmt.Sprintf("运行 %s 超过运行级超时阈值，已被自动终止。\n时间: %s",
						run.CRName, now.Format(time.RFC3339)))
			}()
		}
	}
	return n, nil
}

// RunBackground runs the periodic housekeeping tickers for the run service:
// O-39 run-level timeout sweeps and O-41 queued-run promotion. A zero/negative
// interval disables the loop (returns at once) — by default the interval comes
// from config.RunSweepInterval and a deployment may set it to 0 to opt out.
func (s *PipelineRunService) RunBackground(ctx context.Context, interval time.Duration) {
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
			if _, err := s.SweepRunTimeouts(ctx, time.Now()); err != nil {
				applog.Warnf("run-timeout sweep: %v", err)
			}
			if err := s.SweepQueued(ctx); err != nil {
				applog.Warnf("queued-run sweep: %v", err)
			}
		}
	}
}
