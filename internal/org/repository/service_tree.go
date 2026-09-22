package repository

import (
	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/rouroumaibing/software-distribution-platform-hub/internal/common"
	"github.com/rouroumaibing/software-distribution-platform-hub/internal/org/models"
)

type ServiceTreeRepository struct {
	*common.Repository[models.ServiceTree]
}

func NewServiceTreeRepository(db *gorm.DB) *ServiceTreeRepository {
	return &ServiceTreeRepository{common.NewRepository[models.ServiceTree](db)}
}

func (r *ServiceTreeRepository) GetByOrgID(orgID uuid.UUID) (*models.ServiceTree, error) {
	var st models.ServiceTree
	if err := r.DB.First(&st, "org_id = ?", orgID).Error; err != nil {
		return nil, err
	}
	return &st, nil
}

// ServiceTreeIDByOrg 只回树 id，满足 catalog 侧声明的窄接口
// `service.ServiceTreeLookup`（`GET /orgs/:id/services`，附 A N-9）。
// 为什么不直接让对方用 GetByOrgID：catalog 不需要、也不该依赖 org 的模型类型，
// 两边要共享的事实只有"组织 → 树 id"这一条映射。
func (r *ServiceTreeRepository) ServiceTreeIDByOrg(orgID uuid.UUID) (uuid.UUID, error) {
	st, err := r.GetByOrgID(orgID)
	if err != nil {
		return uuid.Nil, err
	}
	return st.ID, nil
}
