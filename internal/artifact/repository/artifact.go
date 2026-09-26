package repository

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/rouroumaibing/software-distribution-platform-hub/internal/artifact/models"
	"github.com/rouroumaibing/software-distribution-platform-hub/internal/common"
)

type ArtifactRepository struct {
	*common.Repository[models.Artifact]
}

func NewArtifactRepository(db *gorm.DB) *ArtifactRepository {
	return &ArtifactRepository{common.NewRepository[models.Artifact](db)}
}

// liveForComponent is the **listing** predicate. It hides
// CleanupStatePendingDeletion rows: those are logically deleted (expired, with
// object removal still outstanding) and only retained so the GC can retry.
// Listing them would offer a download the GC is trying to remove.
//
// It is a package-level func rather than an inline closure so the query shape can
// be pinned by a DryRun test — the asymmetry with expiredQuery below is the kind
// of thing that breaks silently.
func liveForComponent(db *gorm.DB, componentID uuid.UUID) *gorm.DB {
	return db.Model(&models.Artifact{}).
		Where("component_id = ? AND cleanup_state <> ?", componentID, models.CleanupStatePendingDeletion).
		Order("created_at desc")
}

func (r *ArtifactRepository) FindByComponentID(componentID uuid.UUID, p common.Pagination) ([]models.Artifact, int64, error) {
	return r.List(p, func(db *gorm.DB) *gorm.DB { return liveForComponent(db, componentID) })
}

// ExistsByComponentAndKey reports whether a live (non-cleanup-pending) row
// already registers this storage key for the component. G-14: ApplyStatus
// callbacks fire once per status update, so RegisterProduced needs an
// idempotency probe or every Succeeded re-report inserts a duplicate row
// (2026-09-26 实测：一个 run 重复登记 7 行).
func (r *ArtifactRepository) ExistsByComponentAndKey(componentID uuid.UUID, key string) (bool, error) {
	var n int64
	err := r.DB.Model(&models.Artifact{}).
		Where("component_id = ? AND storage_key = ?", componentID, key).
		Count(&n).Error
	return n > 0, err
}

// expiredQuery is the **GC** predicate: rows whose retention has lapsed, oldest
// first, bounded by limit.
//
// 刻意**不**过滤 CleanupStatePendingDeletion —— 那些行正是上一轮"对象删失败"留下的，
// 本轮的职责就是重试。把过期查询写成与列表查询同样的谓词，重试就**永远不会发生**
// （而这恰好是 cleanup_state 存在的唯一理由）。两个谓词必须不同，这条由 DryRun
// 测试逐字钉住。
func (r *ArtifactRepository) expiredQuery(now time.Time, limit int) *gorm.DB {
	q := r.DB.Model(&models.Artifact{}).
		Where("expires_at IS NOT NULL AND expires_at < ?", now).
		Order("expires_at asc")
	if limit > 0 {
		q = q.Limit(limit)
	}
	return q
}

// FindExpired returns up to limit expired artifacts for the retention GC
// (backlog B-16: 让 `expires_at` 真正生效).
func (r *ArtifactRepository) FindExpired(now time.Time, limit int) ([]models.Artifact, error) {
	var out []models.Artifact
	if err := r.expiredQuery(now, limit).Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

// MarkCleanupPending records that the object could not be removed, so the next
// GC pass retries this row. It is the durable counterpart of the warning logged
// by ArtifactService.Delete — a log line dies with the process, this does not.
func (r *ArtifactRepository) MarkCleanupPending(id uuid.UUID) error {
	return r.DB.Model(&models.Artifact{}).
		Where("id = ?", id).
		Update("cleanup_state", models.CleanupStatePendingDeletion).Error
}

// ListStorageKeys returns every artifact's storage key, ordered, for the
// periodic orphan reconciliation job (backlog B-16 对账).
//
// 刻意**不**过滤 `expires_at`，也**不**过滤 `cleanup_state`：已过期（或对象删失败）
// 但对象仍在的行，同样是"DB 有、对象也还在"的正常状态。把它排除在外会让对账把这
// 类行误报成"缺对象"，并把它们**仍在存储里的对象**误报成"孤儿"。对账只回答"两边
// 集合差在哪"，判断留给人。
func (r *ArtifactRepository) ListStorageKeys() ([]string, error) {
	var keys []string
	err := r.DB.Model(&models.Artifact{}).Order("storage_key asc").Pluck("storage_key", &keys).Error
	return keys, err
}
