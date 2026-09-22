package repository

import (
	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/rouroumaibing/software-distribution-platform-hub/internal/common"
	"github.com/rouroumaibing/software-distribution-platform-hub/internal/environment/models"
)

type EnvironmentRepository struct {
	*common.Repository[models.Environment]
}

func NewEnvironmentRepository(db *gorm.DB) *EnvironmentRepository {
	return &EnvironmentRepository{common.NewRepository[models.Environment](db)}
}

func (r *EnvironmentRepository) FindByComponentID(componentID uuid.UUID, p common.Pagination) ([]models.Environment, int64, error) {
	return r.List(p, func(db *gorm.DB) *gorm.DB {
		return db.Where("component_id = ?", componentID)
	})
}

// ProductionTargets returns the subset of targetIDs that at least one
// environment marks as production. It backs the 生产强审批 guard (B-11): a
// trigger against a production environment must carry an approval gate.
//
// 一次批量查询而不是逐个 target 打一次 —— 触发端点对 fan-out（多环境一次触发）
// 是热路径，N 次查询会把它变成 N+1。
func (r *EnvironmentRepository) ProductionTargets(targetIDs []uuid.UUID) ([]uuid.UUID, error) {
	if len(targetIDs) == 0 {
		return nil, nil
	}
	var out []uuid.UUID
	err := r.DB.Model(&models.Environment{}).
		Where("env_type = ?", models.EnvTypeProduction).
		Where("target_id IN ?", targetIDs).
		Distinct().
		Pluck("target_id", &out).Error
	return out, err
}

// ResolveKey returns an environment's human-readable key (e.g. "beta"), used to
// denormalize an environment_key snapshot onto component_config_history rows
// (B-14 / DELETE-CONTRACT §6.6-2). It returns a plain string rather than
// *models.Environment so the component package can depend on this narrow slice
// without importing environment/models — the same "narrow interface, no
// cross-layer model import" pattern used for the delete-cascade counters.
func (r *EnvironmentRepository) ResolveKey(envID uuid.UUID) (string, error) {
	env, err := r.GetByID(envID)
	if err != nil {
		return "", err
	}
	return env.Key, nil
}
