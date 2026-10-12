package repository

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/rouroumaibing/software-distribution-platform-hub/internal/common"
	"github.com/rouroumaibing/software-distribution-platform-hub/internal/pipeline/models"
)

// TriggerRepository persists PipelineTrigger rows. The scheduler claims due
// cron rows with FOR UPDATE SKIP LOCKED so multiple hub replicas never fire
// the same trigger twice (S-3 ruling).
type TriggerRepository struct {
	*common.Repository[models.PipelineTrigger]
}

func NewTriggerRepository(db *gorm.DB) *TriggerRepository {
	return &TriggerRepository{common.NewRepository[models.PipelineTrigger](db)}
}

func (r *TriggerRepository) FindByPipeline(pipelineID uuid.UUID, p common.Pagination) ([]models.PipelineTrigger, int64, error) {
	return r.List(p, func(db *gorm.DB) *gorm.DB {
		return db.Where("pipeline_id = ?", pipelineID).Order("created_at asc")
	})
}

// FindEnabledByType returns all enabled triggers of one type (scheduler /
// event evaluation scan).
func (r *TriggerRepository) FindEnabledByType(t string) ([]models.PipelineTrigger, error) {
	var out []models.PipelineTrigger
	err := r.DB.Where("type = ? AND enabled = ?", t, true).Find(&out).Error
	return out, err
}

// ClaimDueCron locks all enabled cron triggers (SKIP LOCKED) inside tx and
// returns them; callers decide which are due and write back LastTriggeredAt
// on the SAME tx so claiming + firing + bookkeeping is atomic per tick.
func (r *TriggerRepository) ClaimDueCron(tx *gorm.DB) ([]models.PipelineTrigger, error) {
	var out []models.PipelineTrigger
	err := tx.
		Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).
		Where("type = ? AND enabled = ?", models.TriggerTypeCron, true).
		Find(&out).Error
	return out, err
}

// UpdateLastTriggered stamps the trigger inside the claiming tx.
func (r *TriggerRepository) UpdateLastTriggered(tx *gorm.DB, id uuid.UUID, at time.Time) error {
	return tx.Model(&models.PipelineTrigger{}).Where("id = ?", id).
		Update("last_triggered_at", at).Error
}
