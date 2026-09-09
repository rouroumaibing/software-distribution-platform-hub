package repository

import (
	"github.com/google/uuid"
	"gorm.io/gorm"

	runnerapi "github.com/rouroumaibing/software-distribution-platform-runner/api/v1alpha1"

	"github.com/rouroumaibing/software-distribution-platform-hub/internal/run/models"
)

// TaskRunRepository is read-mostly: rows are written by the sync process
// that consumes status updates relayed from the runner, not by user CRUD.
type TaskRunRepository struct{ DB *gorm.DB }

func NewTaskRunRepository(db *gorm.DB) *TaskRunRepository { return &TaskRunRepository{DB: db} }

func (r *TaskRunRepository) Upsert(t *models.TaskRun) error {
	return r.DB.Save(t).Error
}

func (r *TaskRunRepository) ListByPipelineRunID(pipelineRunID uuid.UUID) ([]models.TaskRun, error) {
	var tasks []models.TaskRun
	err := r.DB.Where("pipeline_run_id = ?", pipelineRunID).Order("created_at asc").Find(&tasks).Error
	return tasks, err
}

// GetByRunAndTaskName returns the single TaskRun row for a DAG node, used by
// the approval gating flow to validate that the targeted task is in fact an
// Approval-type task still awaiting a decision. Returns a gorm not-found
// error when the node isn't part of the run.
func (r *TaskRunRepository) GetByRunAndTaskName(runID uuid.UUID, taskName string) (*models.TaskRun, error) {
	var tr models.TaskRun
	err := r.DB.Where("pipeline_run_id = ? AND task_name = ?", runID, taskName).First(&tr).Error
	return &tr, err
}

// SaveStatus upserts a TaskRun row from a Runner status summary, matched by
// (pipeline_run_id, task_name). On first sight it creates the row (filling
// CRName from the summary); subsequent calls update phase/timing in place.
func (r *TaskRunRepository) SaveStatus(runID uuid.UUID, ts runnerapi.TaskRunStatusSummary) error {
	var existing models.TaskRun
	err := r.DB.Where("pipeline_run_id = ? AND task_name = ?", runID, ts.Name).First(&existing).Error
	if err != nil {
		tr := models.TaskRun{
			PipelineRunID: runID,
			TaskName:      ts.Name,
			CRName:        ts.TaskRunRef,
			Phase:         ts.Phase,
			RetryCount:    int(ts.RetryCount),
			Message:       ts.Message,
		}
		if ts.StartTime != nil {
			t := ts.StartTime.Time
			tr.StartTime = &t
		}
		if ts.CompletionTime != nil {
			t := ts.CompletionTime.Time
			tr.CompletionTime = &t
		}
		return r.DB.Create(&tr).Error
	}
	existing.CRName = ts.TaskRunRef
	existing.Phase = ts.Phase
	existing.RetryCount = int(ts.RetryCount)
	existing.Message = ts.Message
	if ts.StartTime != nil {
		t := ts.StartTime.Time
		existing.StartTime = &t
	}
	if ts.CompletionTime != nil {
		t := ts.CompletionTime.Time
		existing.CompletionTime = &t
	}
	return r.DB.Save(&existing).Error
}
