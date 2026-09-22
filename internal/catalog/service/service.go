package service

import (
	"fmt"
	"net/http"

	"github.com/google/uuid"

	"github.com/rouroumaibing/software-distribution-platform-hub/internal/catalog/models"
	"github.com/rouroumaibing/software-distribution-platform-hub/internal/common"
)

// ActiveRunCounter reports in-flight runs under a service, so ServiceService can
// refuse deletion while a publish is in progress (DELETE-CONTRACT §6.4 #6).
type ActiveRunCounter interface {
	CountActiveByService(serviceID uuid.UUID, phases []string) (int64, error)
}

// ServiceTreeLookup resolves an org to its 1:1 service tree id.
//
// 只回 **id** 而不是 org 模块的 ServiceTree 模型：catalog 与 org 之间要共享的事实
// 只有"这个组织对应哪棵树"这一条，把对方模型类型拉进本包会让两个模块的模型变更
// 互相绑定。*orgrepository.ServiceTreeRepository 结构化满足本接口，无需适配层。
type ServiceTreeLookup interface {
	ServiceTreeIDByOrg(orgID uuid.UUID) (uuid.UUID, error)
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
}

func NewServiceService(repo ServiceStore, runCounter ActiveRunCounter, treeLookup ServiceTreeLookup) *ServiceService {
	return &ServiceService{repo: repo, runCounter: runCounter, treeLookup: treeLookup}
}

func (s *ServiceService) Create(svc *models.Service) error { return s.repo.Create(svc) }

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
// do not block. The service is soft-deleted (Base.DeletedAt); child components
// are managed by their own lifecycles, not cascade-deleted here. The rejection
// carries a structured reasons list for the console.
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
	return s.repo.Delete(id)
}
