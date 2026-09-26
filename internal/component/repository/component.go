package repository

import (
	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/rouroumaibing/software-distribution-platform-hub/internal/common"
	"github.com/rouroumaibing/software-distribution-platform-hub/internal/component/models"
)

// ComponentRepository embeds the generic CRUD repository and adds
// component-specific queries.
type ComponentRepository struct {
	*common.Repository[models.Component]
}

func NewComponentRepository(db *gorm.DB) *ComponentRepository {
	return &ComponentRepository{common.NewRepository[models.Component](db)}
}

// FindByServiceID lists every component under a given service.
func (r *ComponentRepository) FindByServiceID(serviceID uuid.UUID, p common.Pagination) ([]models.Component, int64, error) {
	return r.List(p, func(db *gorm.DB) *gorm.DB {
		return db.Where("service_id = ?", serviceID)
	})
}

// GetOrgID resolves the org a component belongs to via
// component → service → service_tree. Used to denormalize org_id onto
// RBAC rows (DATA-MODEL §7).
//
// G-10 修复（2026-09-26 E2E）：原先用 GORM 的 Raw().Scan(&uuid.UUID) ——
// GORM 对非 struct/基本类型的 dest 走自己的反射扫描路径，把 uuid.UUID
// （[16]byte 数组）按 uint8 元素处理，postgres uuid 列恒报
// "Scan error ... unsupported Scan, storing driver.Value type <nil> into
// *uint8"（每次触发审批 seed 都会命中 → pipeline_approvals 恒 0 行，
// 通知/审计/超时三链路全断）。改为 database/sql 的 Row().Scan：uuid.UUID
// 实现了 sql.Scanner，驱动返回的 string/[]byte 都能正确落进 [16]byte。
func (r *ComponentRepository) GetOrgID(componentID uuid.UUID) (uuid.UUID, error) {
	var orgID uuid.UUID
	row := r.DB.Raw(`
		SELECT st.org_id FROM components c
		JOIN services s ON s.id = c.service_id
		JOIN service_trees st ON st.id = s.service_tree_id
		WHERE c.id = ?`, componentID).Row()
	if err := row.Scan(&orgID); err != nil {
		return uuid.Nil, err
	}
	return orgID, nil
}
