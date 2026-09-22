package service

import (
	"github.com/rouroumaibing/software-distribution-platform-hub/internal/common/logger"
	"github.com/rouroumaibing/software-distribution-platform-hub/internal/permission/models"
	"github.com/rouroumaibing/software-distribution-platform-hub/internal/permission/repository"
)

// AuditReporter persists audit rows. It implements middleware.AuditSink so the
// auth middleware can hand it a finished request's record without importing the
// whole permission service graph. Failures are logged, never surfaced to the
// caller — audit must not block the operation it observed (ACCOUNT-PERMISSION-
// MODEL §6).
type AuditReporter struct {
	repo *repository.AuditLogRepository
}

func NewAuditReporter(repo *repository.AuditLogRepository) *AuditReporter {
	return &AuditReporter{repo: repo}
}

// Record writes the audit entry best-effort. A storage failure degrades to a
// warning; the originating request is never failed on audit's account.
func (r *AuditReporter) Record(entry models.AuditLog) {
	if err := r.repo.Append(&entry); err != nil {
		logger.Warnf("audit: failed to persist entry action=%s resource=%s: %v", entry.Action, entry.ResourceID, err)
	}
}
