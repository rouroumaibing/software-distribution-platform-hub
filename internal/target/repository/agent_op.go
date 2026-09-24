package repository

import (
	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/rouroumaibing/software-distribution-platform-hub/internal/common"
	"github.com/rouroumaibing/software-distribution-platform-hub/internal/target/models"
)

// AgentOpRepository persists hub-issued agent operations (§9.9 / §9.5).
type AgentOpRepository struct{ db *gorm.DB }

func NewAgentOpRepository(db *gorm.DB) *AgentOpRepository { return &AgentOpRepository{db: db} }

func (r *AgentOpRepository) Create(op *models.AgentOp) error {
	return r.db.Create(op).Error
}

func (r *AgentOpRepository) GetByID(id uuid.UUID) (*models.AgentOp, error) {
	var op models.AgentOp
	if err := r.db.First(&op, "id = ?", id).Error; err != nil {
		return nil, err
	}
	return &op, nil
}

func (r *AgentOpRepository) ListByTarget(targetID uuid.UUID, p common.Pagination) ([]models.AgentOp, int64, error) {
	var (
		items []models.AgentOp
		total int64
	)
	if err := r.db.Model(&models.AgentOp{}).Where("target_id = ?", targetID).Count(&total).Error; err != nil {
		return nil, 0, err
	}
	if err := r.db.Where("target_id = ?", targetID).
		Order("created_at DESC").
		Limit(p.PageSize).Offset((p.Page - 1) * p.PageSize).
		Find(&items).Error; err != nil {
		return nil, 0, err
	}
	return items, total, nil
}

// UpdateStatus persists a lifecycle transition reported by the Runner. The
// current status is compared in the WHERE clause so a late report for an op
// that already moved on (or was drained by another hub instance) is a no-op
// rather than a rewind; the caller validates transition legality first.
func (r *AgentOpRepository) UpdateStatus(id uuid.UUID, from, to, message string) error {
	q := r.db.Model(&models.AgentOp{}).
		Where("id = ? AND status = ?", id, from).
		Updates(map[string]any{"status": to, "message": message})
	return q.Error
}

// AppendLog persists one streamed output chunk. Seq is assigned by scanning
// the op's current max — ops are low-volume (one exec at a time per target),
// a COUNT-based max is proportionate; a sequence table would be over-engineering.
func (r *AgentOpRepository) AppendLog(opID uuid.UUID, stream, chunk string) (*models.AgentOpLog, error) {
	var maxSeq int
	if err := r.db.Model(&models.AgentOpLog{}).Where("op_id = ?", opID).
		Select("COALESCE(MAX(seq), 0)").Scan(&maxSeq).Error; err != nil {
		return nil, err
	}
	row := &models.AgentOpLog{OpID: opID, Seq: maxSeq + 1, Stream: stream, Chunk: chunk}
	if err := r.db.Create(row).Error; err != nil {
		return nil, err
	}
	return row, nil
}

// ListLogs returns an op's chunks in arrival order — the SSE replay source.
func (r *AgentOpRepository) ListLogs(opID uuid.UUID) ([]models.AgentOpLog, error) {
	var items []models.AgentOpLog
	if err := r.db.Where("op_id = ?", opID).Order("seq ASC").Find(&items).Error; err != nil {
		return nil, err
	}
	return items, nil
}

// ListQueuedByTarget returns the ops enqueued while the target's Runner was
// offline — the connect-time drain list. install/upgrade are excluded: they
// stay queued until the bootstrap flow picks them up (§9.9).
func (r *AgentOpRepository) ListQueuedByTarget(targetID uuid.UUID) ([]models.AgentOp, error) {
	var items []models.AgentOp
	if err := r.db.Where("target_id = ? AND status = ? AND op_type = ?",
		targetID, models.AgentOpQueued, models.AgentOpExec).
		Order("created_at ASC").Find(&items).Error; err != nil {
		return nil, err
	}
	return items, nil
}
