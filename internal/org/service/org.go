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
	// TODO: 平台级管理员权限校验;组织删除是高危操作,只做软删除,
	// 保留恢复窗口,不允许立刻级联物理清除下属全部资源。
	return s.orgRepo.Delete(id)
}

func (s *OrgService) GetServiceTree(orgID uuid.UUID) (*models.ServiceTree, error) {
	return s.serviceTreeRepo.GetByOrgID(orgID)
}
