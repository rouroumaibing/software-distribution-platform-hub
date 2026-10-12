package repository

import (
	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/rouroumaibing/software-distribution-platform-hub/internal/component/models"
)

// DependencyRepository persists component dependency edges (ui-12 §6.1).
type DependencyRepository struct{ db *gorm.DB }

func NewDependencyRepository(db *gorm.DB) *DependencyRepository { return &DependencyRepository{db: db} }

func (r *DependencyRepository) DB() *gorm.DB { return r.db }

// ListByComponent returns the component's direct edges. upstream=true → 该组件
// 依赖谁；false → 谁依赖该组件（被谁依赖）。
func (r *DependencyRepository) ListByComponent(id uuid.UUID, upstream bool) ([]models.ComponentDependency, error) {
	var out []models.ComponentDependency
	col := "component_id"
	if !upstream {
		col = "depends_on_id"
	}
	err := r.db.Where(col+" = ?", id).Find(&out).Error
	return out, err
}

func (r *DependencyRepository) Delete(componentID, dependsOnID uuid.UUID) error {
	return r.db.Where("component_id = ? AND depends_on_id = ?", componentID, dependsOnID).
		Delete(&models.ComponentDependency{}).Error
}

// ExistsSelf reports whether an identical edge already exists.
func (r *DependencyRepository) Exists(componentID, dependsOnID uuid.UUID) (bool, error) {
	var n int64
	err := r.db.Model(&models.ComponentDependency{}).
		Where("component_id = ? AND depends_on_id = ?", componentID, dependsOnID).
		Count(&n).Error
	return n > 0, err
}

// All returns every edge — the DFS cycle check materializes it in memory
// (治理图规模 = 组件数，量级小，全量载入最简单且无递归 SQL)。
func (r *DependencyRepository) All() ([]models.ComponentDependency, error) {
	var out []models.ComponentDependency
	err := r.db.Find(&out).Error
	return out, err
}
