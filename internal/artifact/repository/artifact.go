package repository

import (
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

func (r *ArtifactRepository) FindByComponentID(componentID uuid.UUID, p common.Pagination) ([]models.Artifact, int64, error) {
	return r.List(p, func(db *gorm.DB) *gorm.DB {
		return db.Where("component_id = ?", componentID).Order("created_at desc")
	})
}

// ListStorageKeys returns every artifact's storage key, ordered, for the
// periodic orphan reconciliation job (backlog B-16 对账).
//
// 刻意**不**过滤 `expires_at`：已过期但对象仍在的行同样是"DB 有、对象也还在"的
// 正常状态（保留策略的清理是另一个动作），把它排除在外会让对账把这类行误报成
// "缺对象"。对账只回答"两边集合差在哪"，判断留给人。
func (r *ArtifactRepository) ListStorageKeys() ([]string, error) {
	var keys []string
	err := r.DB.Model(&models.Artifact{}).Order("storage_key asc").Pluck("storage_key", &keys).Error
	return keys, err
}
