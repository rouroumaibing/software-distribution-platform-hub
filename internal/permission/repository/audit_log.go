package repository

import (
	"time"

	"gorm.io/gorm"

	"github.com/rouroumaibing/software-distribution-platform-hub/internal/common"
	"github.com/rouroumaibing/software-distribution-platform-hub/internal/permission/models"
)

type AuditLogRepository struct {
	*common.Repository[models.AuditLog]
}

func NewAuditLogRepository(db *gorm.DB) *AuditLogRepository {
	return &AuditLogRepository{common.NewRepository[models.AuditLog](db)}
}

// Append writes one audit row. It never returns an error to the caller path
// that would block the operation — the middleware logs and moves on.
func (r *AuditLogRepository) Append(entry *models.AuditLog) error {
	if entry.Timestamp.IsZero() {
		entry.Timestamp = time.Now()
	}
	return r.DB.Create(entry).Error
}

// Recent returns the latest n audit rows (newest first), optionally filtered
// by action prefix. n is clamped to a sane ceiling by the caller.
func (r *AuditLogRepository) Recent(limit int) ([]models.AuditLog, error) {
	var rows []models.AuditLog
	err := r.DB.Order("timestamp DESC").Limit(limit).Find(&rows).Error
	return rows, err
}

// AuditQuery is the read-side filter set for GET /audit-logs (STATUS #19 /
// CONSOLE-UI-GAPS §3.1). Every field is optional — zero values are dropped
// from the WHERE clause, so an empty query degrades to "latest N rows".
type AuditQuery struct {
	Subject      string
	ActionPrefix string // e.g. "approval." matches approval.approve / approval.reject
	ResourceType string
	ResourceID   string
	Since        time.Time
	Until        time.Time
	Limit        int
}

// Query returns audit rows newest-first under the given filters. The limit is
// clamped here (not only in the handler) so every future caller — CLI, report
// export, tests — shares the same ceiling and the same default.
func (r *AuditLogRepository) Query(q AuditQuery) ([]models.AuditLog, error) {
	stmt := r.DB.Model(&models.AuditLog{})
	if q.Subject != "" {
		stmt = stmt.Where("subject = ?", q.Subject)
	}
	if q.ActionPrefix != "" {
		stmt = stmt.Where("action LIKE ?", q.ActionPrefix+"%")
	}
	if q.ResourceType != "" {
		stmt = stmt.Where("resource_type = ?", q.ResourceType)
	}
	if q.ResourceID != "" {
		stmt = stmt.Where("resource_id = ?", q.ResourceID)
	}
	if !q.Since.IsZero() {
		stmt = stmt.Where("timestamp >= ?", q.Since)
	}
	if !q.Until.IsZero() {
		stmt = stmt.Where("timestamp <= ?", q.Until)
	}
	limit := q.Limit
	if limit <= 0 {
		limit = 100
	}
	if limit > 500 {
		limit = 500
	}
	var rows []models.AuditLog
	err := stmt.Order("timestamp DESC").Limit(limit).Find(&rows).Error
	return rows, err
}
