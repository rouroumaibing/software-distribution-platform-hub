package models

import (
	"time"

	"github.com/google/uuid"
)

// TaskRunLog is a single streamed log chunk persisted by the hub as it
// arrives from a Runner over MessageLogChunk. Persisting chunks in the hub DB
// (rather than only archiving LogsRef on the Runner) keeps a run's output
// observable in the console even after the Runner Pod is gone, and gives the
// G2 log-read endpoint (GET /runs/:id/tasks/:name/log) a queryable store.
//
// Chunks are ordered by (created_at, id) so emission order is preserved
// without an explicit sequence counter — the Runner streams them in order
// over a single WebSocket connection per cluster.
type TaskRunLog struct {
	ID            uuid.UUID `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"id"`
	PipelineRunID uuid.UUID `gorm:"type:uuid;not null;index" json:"pipelineRunId"`
	TaskName      string    `gorm:"size:128;not null;index" json:"taskName"`
	Stream        string    `gorm:"size:16" json:"stream,omitempty"` // "stdout" | "stderr"
	Chunk         string    `gorm:"type:text;not null" json:"chunk"`
	CreatedAt     time.Time `json:"createdAt"`
}

// RunLevelLogBucket is the pseudo task name used for chunks a Runner streams
// without a specific task (run-level output such as init/teardown logs).
const RunLevelLogBucket = "__run__"

func (TaskRunLog) TableName() string { return "task_run_logs" }
