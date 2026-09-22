package repository

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/rouroumaibing/software-distribution-platform-hub/internal/run/models"
)

// DispatchJobRepository is the durable store behind the dispatch queue. It
// tracks every attempt to deliver a PipelineRun spec to a Runner, so delivery
// survives a temporarily-offline target and is retried on reconnect / backoff.
type DispatchJobRepository struct{ DB *gorm.DB }

func NewDispatchJobRepository(db *gorm.DB) *DispatchJobRepository {
	return &DispatchJobRepository{DB: db}
}

func (r *DispatchJobRepository) Create(j *models.DispatchJob) error {
	return r.DB.Create(j).Error
}

func (r *DispatchJobRepository) GetByID(id uuid.UUID) (*models.DispatchJob, error) {
	var j models.DispatchJob
	if err := r.DB.First(&j, "id = ?", id).Error; err != nil {
		return nil, err
	}
	return &j, nil
}

// ListPendingByTarget returns jobs still needing delivery for a target
// (pending or failed-but-due), ordered oldest-first, for redelivery when a
// Runner (re)connects.
func (r *DispatchJobRepository) ListPendingByTarget(targetID uuid.UUID) ([]models.DispatchJob, error) {
	var jobs []models.DispatchJob
	err := r.DB.Where("target_id = ? AND state IN (?, ?)", targetID, models.DispatchJobPending, models.DispatchJobFailed).
		Order("created_at asc").Find(&jobs).Error
	return jobs, err
}

// MarkDispatching claims a job for delivery, incrementing its attempt count.
// It only claims when the job is still pending/failed, so concurrent callers
// (reconnect + sweeper) collapse to a single delivery. Returns the number of
// rows actually updated (0 == already claimed by another caller).
func (r *DispatchJobRepository) MarkDispatching(id uuid.UUID) (int64, error) {
	res := r.DB.Model(&models.DispatchJob{}).
		Where("id = ? AND state IN (?, ?)", id, models.DispatchJobPending, models.DispatchJobFailed).
		Updates(map[string]any{
			"state":    models.DispatchJobDispatching,
			"attempts": gorm.Expr("attempts + 1"),
		})
	return res.RowsAffected, res.Error
}

func (r *DispatchJobRepository) MarkDispatched(id uuid.UUID) error {
	return r.DB.Model(&models.DispatchJob{}).
		Where("id = ?", id).
		Updates(map[string]any{"state": models.DispatchJobDispatched, "last_error": "", "next_retry_at": nil}).
		Error
}

func (r *DispatchJobRepository) MarkFailed(id uuid.UUID, lastErr string, nextRetryAt time.Time) error {
	return r.DB.Model(&models.DispatchJob{}).
		Where("id = ?", id).
		Updates(map[string]any{"state": models.DispatchJobFailed, "last_error": lastErr, "next_retry_at": nextRetryAt}).
		Error
}

// MarkDead retires a job whose retries are exhausted.
func (r *DispatchJobRepository) MarkDead(id uuid.UUID, lastErr string) error {
	return r.DB.Model(&models.DispatchJob{}).
		Where("id = ?", id).
		Updates(map[string]any{"state": models.DispatchJobDead, "last_error": lastErr, "next_retry_at": nil}).
		Error
}

// ListDueRetry returns failed jobs whose backoff has elapsed, oldest-next due first.
func (r *DispatchJobRepository) ListDueRetry(now time.Time) ([]models.DispatchJob, error) {
	var jobs []models.DispatchJob
	err := r.DB.Where("state = ? AND next_retry_at <= ?", models.DispatchJobFailed, now).
		Order("next_retry_at asc").Find(&jobs).Error
	return jobs, err
}

// LatestByRun returns the most recently created dispatch job for a run, used by
// Redispatch to copy the last payload for a retry.
func (r *DispatchJobRepository) LatestByRun(pipelineRunID uuid.UUID) (*models.DispatchJob, error) {
	var j models.DispatchJob
	if err := r.DB.Order("created_at desc").First(&j, "pipeline_run_id = ?", pipelineRunID).Error; err != nil {
		return nil, err
	}
	return &j, nil
}
