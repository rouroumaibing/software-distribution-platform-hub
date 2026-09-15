package service

import (
	"github.com/google/uuid"

	"github.com/rouroumaibing/software-distribution-platform-hub/internal/common"
	"github.com/rouroumaibing/software-distribution-platform-hub/internal/component/models"
	"github.com/rouroumaibing/software-distribution-platform-hub/internal/component/repository"
	applog "github.com/rouroumaibing/software-distribution-platform-hub/internal/common/logger"
	permbrepo "github.com/rouroumaibing/software-distribution-platform-hub/internal/permission/repository"
	permmodels "github.com/rouroumaibing/software-distribution-platform-hub/internal/permission/models"
)

// ComponentService satisfies common.CRUDService[models.Component]:
// Create/Get/Update/Delete all match the signatures RegisterCRUD expects.
// Create additionally auto-binds the component owner as component-admin
// (§7.4: the owner is the default approver / administrator).
type ComponentService struct {
	repo              *repository.ComponentRepository
	bindingRepo       *permbrepo.BindingRepository
	componentRoleRepo *permbrepo.ComponentRoleRepository
}

func NewComponentService(
	repo *repository.ComponentRepository,
	bindingRepo *permbrepo.BindingRepository,
	componentRoleRepo *permbrepo.ComponentRoleRepository,
) *ComponentService {
	return &ComponentService{
		repo:              repo,
		bindingRepo:       bindingRepo,
		componentRoleRepo: componentRoleRepo,
	}
}

func (s *ComponentService) Create(c *models.Component) error {
	if err := s.repo.Create(c); err != nil {
		return err
	}
	// §7.4: bind the owner (user or group) as component-admin so they can
	// approve + administer by default. Non-fatal: a bind failure must not
	// roll back component creation.
	if err := s.bindOwner(c); err != nil {
		applog.Warnf("component: auto-bind owner as component-admin failed for %s: %v", c.ID, err)
	}
	return nil
}

// bindOwner grants the component-admin role to the component's owner.
func (s *ComponentService) bindOwner(c *models.Component) error {
	if s.bindingRepo == nil || s.componentRoleRepo == nil {
		return nil
	}
	var subjectType, subjectID string
	switch {
	case c.OwnerUser != nil:
		subjectType, subjectID = "user", c.OwnerUser.String()
	case c.OwnerGroup != nil:
		subjectType, subjectID = "group", *c.OwnerGroup
	default:
		return nil // no owner set → nothing to bind
	}
	admin, err := s.componentRoleRepo.GetByName("component-admin")
	if err != nil {
		return err
	}
	orgID, _ := s.repo.GetOrgID(c.ID)
	b := &permmodels.ComponentRoleBinding{
		ComponentID:     c.ID,
		OrgID:           orgIDPtr(orgID),
		SubjectType:     subjectType,
		SubjectID:       subjectID,
		ComponentRoleID: &admin.ID,
	}
	return s.bindingRepo.Create(b)
}

func orgIDPtr(id uuid.UUID) *uuid.UUID {
	if id == uuid.Nil {
		return nil
	}
	return &id
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
