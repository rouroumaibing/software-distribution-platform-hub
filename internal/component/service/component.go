package service

import (
	"github.com/google/uuid"

	"github.com/rouroumaibing/software-distribution-platform-hub/internal/common"
	"github.com/rouroumaibing/software-distribution-platform-hub/internal/component/models"
	"github.com/rouroumaibing/software-distribution-platform-hub/internal/component/repository"
)

// ComponentService satisfies common.CRUDService[models.Component]:
// Create/Get/Update/Delete all match the signatures RegisterCRUD expects,
// so the handler layer needs zero boilerplate wiring for those four ops.
type ComponentService struct {
	repo *repository.ComponentRepository
}

func NewComponentService(repo *repository.ComponentRepository) *ComponentService {
	return &ComponentService{repo: repo}
}

func (s *ComponentService) Create(c *models.Component) error {
	// TODO: 权限校验(创建者需具备所属 Service 的 create 权限);创建成功后
	// 自动写入 component_role_bindings,把创建者绑定为该组件的 Admin 角色。
	return s.repo.Create(c)
}

func (s *ComponentService) Get(id uuid.UUID) (*models.Component, error) {
	return s.repo.GetByID(id)
}

func (s *ComponentService) ListByService(serviceID uuid.UUID, p common.Pagination) ([]models.Component, int64, error) {
	return s.repo.FindByServiceID(serviceID, p)
}

func (s *ComponentService) Update(id uuid.UUID, c *models.Component) error {
	c.ID = id
	return s.repo.Update(c)
}

func (s *ComponentService) Delete(id uuid.UUID) error {
	// TODO: 权限校验(delete 权限)+ 检查是否还有运行中的 PipelineRun,
	// 避免删除正在发布中的组件。
	return s.repo.Delete(id)
}
