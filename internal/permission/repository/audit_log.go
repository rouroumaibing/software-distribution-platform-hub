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
