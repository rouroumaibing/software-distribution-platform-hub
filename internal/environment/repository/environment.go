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

// UniqueForComponentTargetNamespace resolves the single environment matching
// the trigger-time (component, target, namespace) triple — the run↔environment
// binding behind the 环境覆盖档 approval policy (ui-12 §5.1). ok=false when zero
// OR multiple rows match: an ambiguous binding is treated as "no environment"
// rather than guessed (fail-open, same philosophy as the change-window config
// read — 归属影响的是指派精度与统计维度，不是事故阻断).
func (r *EnvironmentRepository) UniqueForComponentTargetNamespace(componentID, targetID uuid.UUID, namespace string) (envID uuid.UUID, ok bool, err error) {
	var rows []models.Environment
	err = r.DB.Select("id").
		Where("component_id = ? AND target_id = ? AND namespace = ?", componentID, targetID, namespace).
		Limit(2).
		Find(&rows).Error
	if err != nil {
		return uuid.Nil, false, err
	}
	if len(rows) != 1 {
		return uuid.Nil, false, nil
	}
	return rows[0].ID, true, nil
}
