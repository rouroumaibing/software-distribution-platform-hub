package service

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/rouroumaibing/software-distribution-platform-hub/internal/common"
	"github.com/rouroumaibing/software-distribution-platform-hub/internal/common/logger"
	"github.com/rouroumaibing/software-distribution-platform-hub/internal/keycloak"
	"github.com/rouroumaibing/software-distribution-platform-hub/internal/org/models"
	"github.com/rouroumaibing/software-distribution-platform-hub/internal/org/repository"
)

// OrgService satisfies common.CRUDService[models.Org].
type OrgService struct {
	orgRepo         *repository.OrgRepository
	serviceTreeRepo *repository.ServiceTreeRepository
	// groupProvisioner is optional. When nil (dev mode / no Keycloak admin
	// secret), org carrier groups are not synced to Keycloak.
	groupProvisioner keycloak.GroupProvisioner
}

func NewOrgService(orgRepo *repository.OrgRepository, serviceTreeRepo *repository.ServiceTreeRepository, groupProvisioner keycloak.GroupProvisioner) *OrgService {
	return &OrgService{orgRepo: orgRepo, serviceTreeRepo: serviceTreeRepo, groupProvisioner: groupProvisioner}
}

// Create creates an Org and its 1:1 ServiceTree together, so callers never
// have to remember to create the tree separately. It also best-effort
// provisions the org carrier group `/org:<slug>` in Keycloak when a
// provisioner is wired (see provisionOrgGroup). A group-sync failure never
// fails the request: the hub DB row is the source of truth; the Keycloak group
// is a convenience carrier for the token `groups` claim (ACCOUNT-PERMISSION-
// MODEL §2.3).
func (s *OrgService) Create(o *models.Org) error {
	if err := s.orgRepo.Create(o); err != nil {
		return err
	}
	if err := s.serviceTreeRepo.Create(&models.ServiceTree{OrgID: o.ID, Name: "default"}); err != nil {
		return err
	}
	s.provisionOrgGroup(o.Slug)
	return nil
}

// provisionOrgGroup creates the org carrier group `/org:<slug>` in Keycloak.
// Failure is logged, never returned: Keycloak may be transiently unavailable,
// but org creation must still succeed. A bounded context avoids hanging on a
// dead Keycloak during a request path that has no caller context of its own.
func (s *OrgService) provisionOrgGroup(slug string) {
	if s.groupProvisioner == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := s.groupProvisioner.EnsureGroup(ctx, "/org:"+slug); err != nil {
		logger.Warnf("org: ensure keycloak group /org:%s failed (non-fatal): %v", slug, err)
	}
}

// ReconcileGroups ensures every existing org already has its `/org:<slug>`
// carrier group. It is idempotent and best-effort, used for the one-time
// startup backfill of orgs created before group provisioning existed. Errors
// are logged, never fatal — a Keycloak outage must not block hub startup.
func (s *OrgService) ReconcileGroups(ctx context.Context) {
	if s.groupProvisioner == nil {
		return
	}
	const pageSize = 500
	for page := 1; ; page++ {
		orgs, total, err := s.List(common.Pagination{Page: page, PageSize: pageSize})
		if err != nil {
			logger.Warnf("org: reconcile groups list failed: %v", err)
			return
		}
		for i := range orgs {
			if err := s.groupProvisioner.EnsureGroup(ctx, "/org:"+orgs[i].Slug); err != nil {
				logger.Warnf("org: reconcile group /org:%s failed: %v", orgs[i].Slug, err)
			}
		}
		if int64(page*pageSize) >= total {
			break
		}
	}
}

func (s *OrgService) Get(id uuid.UUID) (*models.Org, error) { return s.orgRepo.GetByID(id) }

// List 读时自举：组织列表为空时就地补插默认组织（含 1:1 服务树）后重新
// 查询——模式对位 old pkg/models/service_servicetree.go GetServiceTree 的
// "表空则插入 DefaultServiceTreeTableString 并重查"，首次使用页面不为空。
// 并发下重复插入会被 slug 唯一索引挡住，重查即可。
func (s *OrgService) List(p common.Pagination) ([]models.Org, int64, error) {
	orgs, total, err := s.orgRepo.List(p)
	if err != nil {
		return nil, 0, err
	}
	if total > 0 {
		return orgs, total, nil
	}
	def := models.DefaultOrg()
	if createErr := s.Create(def); createErr != nil {
		// 唯一索引冲突等场景：另一个请求已在补插，直接回落到重查。
		logger.Warnf("org: seed default org skipped: %v", createErr)
	}
	return s.orgRepo.List(p)
}

func (s *OrgService) Update(id uuid.UUID, o *models.Org) error {
	o.ID = id
	return s.orgRepo.Update(o)
}

func (s *OrgService) Delete(id uuid.UUID) error {
	// 平台级管理员权限校验在路由层完成：本方法只经 OrgHandler.RegisterAdminRoutes
	// 挂载于 platform-admin 路由组（与 componentRoleHandler.RegisterAdminRoutes 同手法），
	// 该组在 auth 开启时挂 RequirePlatformPermission，dev 模式（auth==nil）下不加守卫——
	// 与 ACCOUNT-PERMISSION-MODEL §11 步骤 4 的平台守卫收口一致。故此处不再重复鉴权。
	//
	// 删除语义（DELETE-CONTRACT §6.4 表格 #1，已逐项拍板）：
	//   - Org 只做**软删除**（common.Base 含 gorm.DeletedAt，GORM 自动置 deleted_at，
	//     保留恢复窗口），**不**立刻级联物理清除下属资源；
	//   - **不**对 service 做级联（软删或硬删）：物理资源清空走独立运维流程，org
	//     删除保持轻量、可恢复。这是 §6.4 与文档末尾四句建议之间的歧义点，按更具体、
	//     且已逐项拍板的表格执行（详见 internal/cascade 包注释）。
	//   - 若确需清掉某 org 的全部资源，调用方应在 **service 层**走
	//     cascade.Deleter.DeleteServiceSubtree（已落：service→component→artifacts
	//     同事务级联软删 + 制品 GC 标记，并含"无活跃运行"事务内守卫），而非经 org 删除。
	return s.orgRepo.Delete(id)
}

func (s *OrgService) GetServiceTree(orgID uuid.UUID) (*models.ServiceTree, error) {
	return s.serviceTreeRepo.GetByOrgID(orgID)
}
