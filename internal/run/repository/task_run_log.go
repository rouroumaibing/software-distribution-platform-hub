package repository

import (
	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/rouroumaibing/software-distribution-platform-hub/internal/common"
	"github.com/rouroumaibing/software-distribution-platform-hub/internal/run/models"
)

// TaskRunLogRepository stores streamed log chunks for later retrieval by the
// G2 log-read endpoint. It is append-only: chunks arrive in order and are
// never mutated.
type TaskRunLogRepository struct{ DB *gorm.DB }

func NewTaskRunLogRepository(db *gorm.DB) *TaskRunLogRepository {
	return &TaskRunLogRepository{DB: db}
}

func (r *TaskRunLogRepository) Append(log *models.TaskRunLog) error {
	return r.DB.Create(log).Error
}

// ListByRunTask returns the ordered log chunks for a (run, task) pair,
// paginated by creation order so the console can tail without pulling the
// whole history on every poll.
func (r *TaskRunLogRepository) ListByRunTask(runID uuid.UUID, taskName string, p common.Pagination) ([]models.TaskRunLog, int64, error) {
	return common.NewRepository[models.TaskRunLog](r.DB).List(p, func(db *gorm.DB) *gorm.DB {
		return db.Where("pipeline_run_id = ? AND task_name = ?", runID, taskName).Order("created_at asc, id asc")
	})
}
