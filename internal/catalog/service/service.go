package service

import (
	"fmt"
	"net/http"

	"github.com/google/uuid"

	"github.com/rouroumaibing/software-distribution-platform-hub/internal/cascade"
	"github.com/rouroumaibing/software-distribution-platform-hub/internal/catalog/models"
	"github.com/rouroumaibing/software-distribution-platform-hub/internal/common"
	"gorm.io/gorm"
)

// ActiveRunCounter reports in-flight runs under a service, so ServiceService can
// refuse deletion while a publish is in progress (DELETE-CONTRACT §6.4 #6).
type ActiveRunCounter interface {
	CountActiveByService(serviceID uuid.UUID, phases []string) (int64, error)
	// CountActiveByServiceTx 是同一计数在调用方事务视图上的版本，用于把判定
	// 收进级联删除同一事务（关闭 §1.3 并发插入绕过窗口）。
	CountActiveByServiceTx(tx *gorm.DB, serviceID uuid.UUID, phases []string) (int64, error)
}

// ServiceTreeLookup resolves an org to its 1:1 service tree id, and reports
// whether a service tree row exists (D-02 parent-liveness guard).
//
// 只回 **id** 而不是 org 模块的 ServiceTree 模型：catalog 与 org 之间要共享的事实
// 只有"这个组织对应哪棵树"这一条，把对方模型类型拉进本包会让两个模块的模型变更
// 互相绑定。*orgrepository.ServiceTreeRepository 结构化满足本接口，无需适配层。
type ServiceTreeLookup interface {
	ServiceTreeIDByOrg(orgID uuid.UUID) (uuid.UUID, error)
	// ServiceTreeExists reports whether a service tree row exists. A deleted tree
	// counts as non-existent (soft-delete aware), matching the FK semantics a
	// missing row would give. Used by Create to reject a dangling serviceTreeId
	// before write (D-02, aligned with B-15's parent-liveness guard).
	ServiceTreeExists(treeID uuid.UUID) (bool, error)
}

// activePhases are run phases that block deletion (functional safety).
var activePhases = []string{"Pending", "Running", "WaitingApproval"}

// ServiceStore is the persistence surface ServiceService needs. An interface
// (same pattern as StageService's StageStore / ComponentService's
// ComponentStore) so the delete guard can be unit-tested without Postgres;
// *repository.ServiceRepository satisfies it as-is.
type ServiceStore interface {
	Create(*models.Service) error
	GetByID(uuid.UUID) (*models.Service, error)
	Update(*models.Service) error
	Delete(uuid.UUID) error
	FindByServiceTreeID(uuid.UUID, common.Pagination) ([]models.Service, int64, error)
}

// ServiceService satisfies common.CRUDService[models.Service].
type ServiceService struct {
	repo       ServiceStore
	runCounter ActiveRunCounter
	treeLookup ServiceTreeLookup
	// cascader 是可选的域内级联删除器（§6.4 #2：删服务 = 同事务级联软删其下全部
	// 组件及组件子资源）。setter 注入原因同 ComponentService：5 处既有测试用
	// 3 参构造；nil 时退回单表软删（脱库单测路径）。
	cascader *cascade.Deleter
}

func NewServiceService(repo ServiceStore, runCounter ActiveRunCounter, treeLookup ServiceTreeLookup) *ServiceService {
	return &ServiceService{repo: repo, runCounter: runCounter, treeLookup: treeLookup}
}

// WithCascade 装配域内级联删除器（main.go 在 DB 就绪后调用）。
func (s *ServiceService) WithCascade(d *cascade.Deleter) *ServiceService {
	s.cascader = d
	return s
}

// Create validates the referenced service tree exists before persisting, so a
// dangling serviceTreeId can never become a row (D-02, aligned with B-15's
// parent-liveness guard). The FK on services.service_tree_id already enforces
// this at the DB level, but an explicit check turns a later FK error into a
// clear 400 and keeps the invariant even if constraints are relaxed or the
// create path bypasses the FK somehow.
//
// treeLookup==nil 时退回不校验（兼容脱库/未装配路径，与 Delete 的 runCounter
// 守卫同口径）；ServiceTreeID==uuid.Nil 单独报 400。
func (s *ServiceService) Create(svc *models.Service) error {
	if svc.ServiceTreeID == uuid.Nil {
		return common.DomainError(common.KindService, http.StatusBadRequest, 2,
			"serviceTreeId is required")
	}
	if s.treeLookup != nil {
		ok, err := s.treeLookup.ServiceTreeExists(svc.ServiceTreeID)
		if err != nil {
			return err
		}
		if !ok {
			return common.DomainError(common.KindService, http.StatusBadRequest, 3,
				"serviceTreeId does not exist")
		}
	}
	return s.repo.Create(svc)
}

func (s *ServiceService) Get(id uuid.UUID) (*models.Service, error) { return s.repo.GetByID(id) }

func (s *ServiceService) ListByServiceTree(treeID uuid.UUID, p common.Pagination) ([]models.Service, int64, error) {
	return s.repo.FindByServiceTreeID(treeID, p)
}

// ListByOrg lists the services directly under an org (`GET /orgs/:id/services`,
// CONSOLE-UI-DESIGN.md 附 A N-9).
//
// 存在的意义是替调用方省一跳：服务树是 org 的 1:1 影子，前端若只有
// `/service-trees/:id/services`，每展开一个组织都要先 `GET /orgs/:id/service-tree`
// 拿到树 id 再列服务。懒加载恰好是"每展开一次多一跳"的场景，省掉的正是这一跳。
func (s *ServiceService) ListByOrg(orgID uuid.UUID, p common.Pagination) ([]models.Service, int64, error) {
	if s.treeLookup == nil {
		// 构造期漏装配会在这里变成 nil panic —— 显式报 500 而不是崩进程。
		return nil, 0, common.DomainError(common.KindCatalog, http.StatusInternalServerError, 3,
			"catalog: service-tree lookup is not configured")
	}
	treeID, err := s.treeLookup.ServiceTreeIDByOrg(orgID)
	if err != nil {
		return nil, 0, err
	}
	return s.repo.FindByServiceTreeID(treeID, p)
}

func (s *ServiceService) Update(id uuid.UUID, svc *models.Service) error {
	svc.ID = id
	return s.repo.Update(svc)
}

// Delete refuses to drop a service that still has an in-flight run anywhere
// under it (the only hard rule from DELETE-CONTRACT §6.4 #6). Historical runs
// do not block. The rejection carries a structured reasons list for the console.
//
// 子资源处理（§6.4 #2）：装配了 cascader 时走**域内级联** —— 同一事务内先对服务
// 下每个组件执行整套子资源清理（pipeline/stage/template 软删、environment/config/
// binding 硬删、artifact 打清理标记），再软删组件、最后软删服务自身；未装配时退回
// 旧行为（只软删 service，脱库单测路径）。org 层**不**级联（§6.4 表格 #1 拍板）。
//
// 活跃运行判定与级联删除现处于**同一事务**（STATUS §2 #12）：fast-path 即时 409 +
// 级联事务内 guard 复检，并发新起的 run 会被 guard 看到、整事务回滚。
func (s *ServiceService) Delete(id uuid.UUID) error {
	if s.runCounter != nil {
		n, err := s.runCounter.CountActiveByService(id, activePhases)
		if err != nil {
			return err
		}
		if n > 0 {
			return common.DomainErrorWithReasons(common.KindService, http.StatusConflict, 1,
				"service still has in-progress runs; terminate them before deleting",
				fmt.Sprintf("%d 条运行仍在进行中（Pending/Running/WaitingApproval），请先终止服务下相关组件的运行", n))
		}
	}
	if s.cascader != nil {
		return s.cascader.DeleteServiceSubtree(id, func(tx *gorm.DB) error {
			n, err := s.runCounter.CountActiveByServiceTx(tx, id, activePhases)
			if err != nil {
				return err
			}
			if n > 0 {
				return common.DomainErrorWithReasons(common.KindService, http.StatusConflict, 1,
					"service still has in-progress runs; terminate them before deleting",
					fmt.Sprintf("%d 条运行仍在进行中（Pending/Running/WaitingApproval），请先终止服务下相关组件的运行", n))
			}
			return nil
		})
	}
	return s.repo.Delete(id)
}
