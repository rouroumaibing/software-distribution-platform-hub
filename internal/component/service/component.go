package service

import (
	"fmt"
	"net/http"

	"github.com/google/uuid"

	"github.com/rouroumaibing/software-distribution-platform-hub/internal/common"
	applog "github.com/rouroumaibing/software-distribution-platform-hub/internal/common/logger"
	"github.com/rouroumaibing/software-distribution-platform-hub/internal/component/models"
	permmodels "github.com/rouroumaibing/software-distribution-platform-hub/internal/permission/models"
	permbrepo "github.com/rouroumaibing/software-distribution-platform-hub/internal/permission/repository"
)

// ActiveRunCounter reports in-flight runs under a component, so ComponentService
// can refuse deletion while a publish is in progress (DELETE-CONTRACT §6.4 #6).
type ActiveRunCounter interface {
	CountActiveByComponent(componentID uuid.UUID, phases []string) (int64, error)
}

// activePhases are run phases that block deletion (functional safety).
var activePhases = []string{"Pending", "Running", "WaitingApproval"}

// ComponentStore is the persistence surface ComponentService needs. An
// interface (like StageService's StageStore and the run package's stores) so
// the delete guard can be unit-tested without Postgres;
// *repository.ComponentRepository satisfies it as-is.
type ComponentStore interface {
	Create(*models.Component) error
	GetByID(uuid.UUID) (*models.Component, error)
	Update(*models.Component) error
	Delete(uuid.UUID) error
	FindByServiceID(uuid.UUID, common.Pagination) ([]models.Component, int64, error)
	GetOrgID(uuid.UUID) (uuid.UUID, error)
}

// ComponentService satisfies common.CRUDService[models.Component]:
// Create/Get/Update/Delete all match the signatures RegisterCRUD expects.
// Create additionally auto-binds the component owner as component-admin
// (§7.4: the owner is the default approver / administrator).
type ComponentService struct {
	repo              ComponentStore
	bindingRepo       *permbrepo.BindingRepository
	componentRoleRepo *permbrepo.ComponentRoleRepository
	runCounter        ActiveRunCounter
}

func NewComponentService(
	repo ComponentStore,
	bindingRepo *permbrepo.BindingRepository,
	componentRoleRepo *permbrepo.ComponentRoleRepository,
	runCounter ActiveRunCounter,
) *ComponentService {
	return &ComponentService{
		repo:              repo,
		bindingRepo:       bindingRepo,
		componentRoleRepo: componentRoleRepo,
		runCounter:        runCounter,
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

// Delete refuses to drop a component that still has an in-flight run (the only
// hard rule from DELETE-CONTRACT §6.4 #6): deleting while a publish is running
// would orphan an in-progress Job. Historical runs (Succeeded/Failed/Cancelled)
// do not block. The component is soft-deleted (Base.DeletedAt); its child
// pipelines/environments are managed by their own lifecycles, not cascade
// deleted here. The rejection carries a structured reasons list for the console.
func (s *ComponentService) Delete(id uuid.UUID) error {
	if s.runCounter != nil {
		n, err := s.runCounter.CountActiveByComponent(id, activePhases)
		if err != nil {
			return err
		}
		if n > 0 {
			return common.DomainErrorWithReasons(common.KindComponent, http.StatusConflict, 1,
				"component still has in-progress runs; terminate them before deleting",
				fmt.Sprintf("%d 条运行仍在进行中（Pending/Running/WaitingApproval），请先终止相关流水线运行", n))
		}
	}
	return s.repo.Delete(id)
}
