// Package retention implements the data-lifecycle garbage collectors for data
// that has no retention today: audit logs, pipeline/task run logs, and
// historical artifact versions. It mirrors the shape of artifact/service.GC
// (default-off, batched, failures logged-and-swallowed) but there is no object
// store to coordinate — these are pure DB row deletions, so each pass is a
// bounded DELETE.
//
// 设计要点（与 O-37 设计草案一致）：
//   - 默认全关：三个阈值任一 >0 才删对应数据类型；删数据的作业绝不由一次部署悄悄打开。
//   - 仅删「终态 + 超龄」的运行日志：非终态 run 一律不动，防误删进行中数据。
//   - 审计日志按 created_at 超龄删（合规长留，阈值通常最大）。
//   - 制品版本按「每 component 保留最新 N 个」滚删，避免历史版本无限堆积。
package retention

import (
	"context"
	"time"

	applog "github.com/rouroumaibing/software-distribution-platform-hub/internal/common/logger"
	"gorm.io/gorm"
)

// terminalPhases are the PipelineRun phases after which a run is immutable and
// safe to garbage-collect (mirrors runnerapi.PipelineRunPhase terminal set).
var terminalPhases = []string{"Succeeded", "Failed", "Error", "Canceled"}

// RetentionGC holds the DB handle and the three retention thresholds.
type RetentionGC struct {
	db *gorm.DB

	// AuditLogDays: delete audit_logs older than N days. 0 = keep forever.
	AuditLogDays int
	// RunLogDays: delete terminal pipeline_runs/task_runs finished older than N
	// days. 0 = keep forever.
	RunLogDays int
	// ArtifactVersions: keep the latest N artifact rows per component, delete
	// the rest (oldest first). 0 = keep all.
	ArtifactVersions int

	// Batch bounds how many rows one DELETE touches (LIMIT). Zero falls back to
	// DefaultBatch. A housekeeping DELETE must not lock the table for seconds.
	Batch int
}

// DefaultBatch bounds a single retention DELETE.
const DefaultBatch = 1000

// NewRetentionGC builds the collector.
func NewRetentionGC(db *gorm.DB, auditDays, runDays, artifactVersions, batch int) *RetentionGC {
	if batch <= 0 {
		batch = DefaultBatch
	}
	return &RetentionGC{
		db:               db,
		AuditLogDays:     auditDays,
		RunLogDays:       runDays,
		ArtifactVersions: artifactVersions,
		Batch:            batch,
	}
}

// Disabled reports whether every threshold is off — the caller can skip even
// starting the loop.
func (g *RetentionGC) Disabled() bool {
	return g == nil || (g.AuditLogDays <= 0 && g.RunLogDays <= 0 && g.ArtifactVersions <= 0)
}

// Run periodically collects until ctx is cancelled. A zero/negative interval
// disables the loop.
func (g *RetentionGC) Run(ctx context.Context, interval time.Duration) {
	if g == nil || interval <= 0 || g.Disabled() {
		return
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			g.collectOnce(time.Now())
		}
	}
}

func (g *RetentionGC) collectOnce(now time.Time) {
	if g.db == nil {
		return
	}
	if g.AuditLogDays > 0 {
		cutoff := now.AddDate(0, 0, -g.AuditLogDays)
		res := g.db.Exec(
			`DELETE FROM audit_logs WHERE created_at < ? AND ctid IN (SELECT ctid FROM audit_logs WHERE created_at < ? LIMIT ?)`,
			cutoff, cutoff, g.Batch)
		if res.Error != nil {
			applog.Warnf("retention: audit-log GC failed: %v", res.Error)
		} else if res.RowsAffected > 0 {
			applog.Infof("retention: deleted %d audit_logs older than %d days", res.RowsAffected, g.AuditLogDays)
		}
	}
	if g.RunLogDays > 0 {
		cutoff := now.AddDate(0, 0, -g.RunLogDays)
		// 仅删终态 run；非终态（Pending/Running）一律不动。优先按 completion_time
		// 判龄，缺失时回退 updated_at，双保险防误删进行中运行。
		var prIDs []string
		g.db.Table("pipeline_runs").
			Where("phase IN ?", terminalPhases).
			Where("(completion_time IS NOT NULL AND completion_time < ?) OR (completion_time IS NULL AND updated_at < ?)", cutoff, cutoff).
			Limit(g.Batch).
			Pluck("id::text", &prIDs)
		if len(prIDs) > 0 {
			if err := g.db.Exec("DELETE FROM task_runs WHERE pipeline_run_id = ANY(?)", prIDs).Error; err != nil {
				applog.Warnf("retention: task_run GC failed: %v", err)
			}
			res := g.db.Exec("DELETE FROM pipeline_runs WHERE id = ANY(?)", prIDs)
			if res.Error != nil {
				applog.Warnf("retention: pipeline_run GC failed: %v", res.Error)
			} else if res.RowsAffected > 0 {
				applog.Infof("retention: deleted %d terminal pipeline_runs older than %d days", res.RowsAffected, g.RunLogDays)
			}
		}
	}
	if g.ArtifactVersions > 0 {
		// 每 component 保留最新 N 个制品版本，其余（最旧）滚删。用窗口函数
		// 标记 rn>N 的过期行后删除。
		res := g.db.Exec(`
			DELETE FROM artifacts
			WHERE id IN (
				SELECT id FROM (
					SELECT id, ROW_NUMBER() OVER (
						PARTITION BY component_id ORDER BY created_at DESC
					) AS rn FROM artifacts
				) t WHERE rn > ?
			)`, g.ArtifactVersions)
		if res.Error != nil {
			applog.Warnf("retention: artifact-version GC failed: %v", res.Error)
		} else if res.RowsAffected > 0 {
			applog.Infof("retention: rolled %d artifact versions beyond latest %d/component", res.RowsAffected, g.ArtifactVersions)
		}
	}
}
