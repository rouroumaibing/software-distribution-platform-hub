package service

import (
	"context"
	"time"

	"github.com/rouroumaibing/software-distribution-platform-hub/internal/common/logger"
	"github.com/rouroumaibing/software-distribution-platform-hub/internal/permission/repository"
)

// BindingReaper is the periodic reclaim job for expired grants
// (ACCOUNT-PERMISSION-MODEL §7.4). Authorization already ignores expired rows
// (both ListMatching paths filter on expiry), so this job is purely the
// physical cleanup that stops the binding tables growing without bound. It is
// safe to run as often as you like: deleting an already-absent row is a no-op.
type BindingReaper struct {
	componentRepo *repository.BindingRepository
	platformRepo  *repository.PlatformRoleBindingRepository
}

func NewBindingReaper(
	componentRepo *repository.BindingRepository,
	platformRepo *repository.PlatformRoleBindingRepository,
) *BindingReaper {
	return &BindingReaper{componentRepo: componentRepo, platformRepo: platformRepo}
}

// ReapOnce deletes expired rows from both binding tables as of now, returning
// how many each table dropped. now is a parameter (not time.Now()) so tests can
// pin the cutoff.
func (r *BindingReaper) ReapOnce(now time.Time) (component, platform int64, err error) {
	component, err = r.componentRepo.DeleteExpired(now)
	if err != nil {
		return 0, 0, err
	}
	platform, err = r.platformRepo.DeleteExpired(now)
	if err != nil {
		return component, 0, err
	}
	return component, platform, nil
}

// Run reaps immediately, then every interval until ctx is cancelled. A
// zero/negative interval disables the loop (returns at once) so a misconfigured
// value never spins.
func (r *BindingReaper) Run(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		return
	}
	reap := func() {
		comp, plat, err := r.ReapOnce(time.Now())
		if err != nil {
			logger.Warnf("reaper: expired-grant cleanup failed: %v", err)
			return
		}
		if comp > 0 || plat > 0 {
			logger.Infof("reaper: reclaimed expired grants component=%d platform=%d", comp, plat)
		}
	}
	reap()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			reap()
		}
	}
}
