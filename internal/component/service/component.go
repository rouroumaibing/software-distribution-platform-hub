package service

import (
	"fmt"
	"net/http"

	"github.com/google/uuid"

	"github.com/rouroumaibing/software-distribution-platform-hub/internal/cascade"
	"github.com/rouroumaibing/software-distribution-platform-hub/internal/common"
	applog "github.com/rouroumaibing/software-distribution-platform-hub/internal/common/logger"
	"github.com/rouroumaibing/software-distribution-platform-hub/internal/component/models"
	permmodels "github.com/rouroumaibing/software-distribution-platform-hub/internal/permission/models"
	permbrepo "github.com/rouroumaibing/software-distribution-platform-hub/internal/permission/repository"
	"gorm.io/gorm"
)

// ActiveRunCounter reports in-flight runs under a component, so ComponentService
// can refuse deletion while a publish is in progress (DELETE-CONTRACT §6.4 #6).
type ActiveRunCounter interface {
	CountActiveByComponent(componentID uuid.UUID, phases []string) (int64, error)
	// CountActiveByComponentTx 是同一计数在调用方事务视图上的版本，用于把判定
	// 收进级联删除同一事务（关闭 §1.3 并发插入绕过窗口）。
	CountActiveByComponentTx(tx *gorm.DB, componentID uuid.UUID, phases []string) (int64, error)
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
	// cascader 是可选的域内级联删除器（DELETE-CONTRACT §6.4 #3/#4/#5/#10）。
	// 用 setter 注入而非构造参数：8 处既有测试用 4 参构造，改签名会全部波及；
	// 且级联器是"装配增强"，nil 时行为退回单表软删（脱库单测路径）。
	cascader *cascade.Deleter
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

// WithCascade 装配域内级联删除器（main.go 在 DB 就绪后调用）。
func (s *ComponentService) WithCascade(d *cascade.Deleter) *ComponentService {
	s.cascader = d
	return s
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
	case c.OwnerSub != nil:
		subjectType, subjectID = "user", *c.OwnerSub
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
// do not block. The rejection carries a structured reasons list for the console.
//
// 子资源处理（§6.4）：装配了 cascader 时走**域内级联**（同一事务内软删
// pipelines/stages/task_templates、硬删 environments/configs/bindings、给
// artifacts 打 pending_deletion+expires_at 清理标记）；未装配时退回旧行为——
// 只软删 component 自身（脱库单测与无 DB 装配路径）。
//
// 残余竞态已关闭（STATUS §2 #12，2026-09-25）：下方 fast-path 判定用于即时 409 UX；
// 真正的原子性由级联事务内的 guard 保证 —— 删除组件/服务时，活跃运行计数在**同一
// 事务**里复检，并发插入的 run 会被 guard 看到、整事务回滚、返回结构化 409。
// 设计裁定（双向钢人论证）：把计数收进级联事务是值得的——窗口虽小、后果虽"只是
// 历史 run 挂在软删组件上"，但它是 DELETE-CONTRACT §6.4 #6 唯一硬规则的实质性
// 例外路径，原子化后该硬规则不再有可绕过口子，且 run repo 已支持 tx 绑定、改动局部。
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
	if s.cascader != nil {
		return s.cascader.DeleteComponentSubtree(id, func(tx *gorm.DB) error {
			n, err := s.runCounter.CountActiveByComponentTx(tx, id, activePhases)
			if err != nil {
				return err
			}
			if n > 0 {
				return common.DomainErrorWithReasons(common.KindComponent, http.StatusConflict, 1,
					"component still has in-progress runs; terminate them before deleting",
					fmt.Sprintf("%d 条运行仍在进行中（Pending/Running/WaitingApproval），请先终止相关流水线运行", n))
			}
			return nil
		})
	}
	return s.repo.Delete(id)
}
