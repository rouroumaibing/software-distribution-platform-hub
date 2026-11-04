package service

import (
	"context"
	"fmt"
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
	// kc is the Keycloak Admin client. When nil (dev mode / no Keycloak admin
	// secret) org carrier groups are not synced; the hub DB row remains the
	// source of truth and org creation still succeeds.
	kc *keycloak.Client
}

func NewOrgService(orgRepo *repository.OrgRepository, serviceTreeRepo *repository.ServiceTreeRepository, kc *keycloak.Client) *OrgService {
	return &OrgService{orgRepo: orgRepo, serviceTreeRepo: serviceTreeRepo, kc: kc}
}

// Create creates an Org as a transactional three-piece set (ACCESS-CONTROL
// §3.2): (1) Keycloak top group `/{slug}`, (2) `admins` subgroup with the
// org_manager user added + `org_admin` attribute written, (3) DB rows (org +
// 1:1 service tree + annotations/quotas). Keycloak provisioning runs FIRST;
// if it fails the DB rows are never written (the whole set rolls back). If the
// DB write fails after Keycloak succeeded, the Keycloak groups are best-effort
// cleaned up so a later retry starts clean. This ordering is what makes the
// "any failure rolls back the whole set" gate hold.
func (s *OrgService) Create(o *models.Org) error {
	mgr := o.DecodeAnnotations().OrgManager
	if s.kc != nil && mgr != "" {
		if err := s.provisionOrgKeycloak(o.Slug, mgr); err != nil {
			return fmt.Errorf("org: keycloak provisioning failed (db not written): %w", err)
		}
	}
	if err := s.orgRepo.Create(o); err != nil {
		if s.kc != nil {
			s.cleanupOrgKeycloak(o.Slug)
		}
		return err
	}
	if err := s.serviceTreeRepo.Create(&models.ServiceTree{OrgID: o.ID, Name: "default"}); err != nil {
		if s.kc != nil {
			s.cleanupOrgKeycloak(o.Slug)
		}
		_ = s.orgRepo.Delete(o.ID)
		return err
	}
	return nil
}

// provisionOrgKeycloak builds the Keycloak side of an org: top group `/{slug}`,
// `admins` subgroup, adds the manager into it, and writes the manager's
// `org_admin` attribute (L2 membership, ACCESS-CONTROL §1.2). It is idempotent
// on re-run: EnsureGroup/CreateSubGroup/AddGroupMember/SetUserAttributes are
// all no-ops when the entity already exists.
func (s *OrgService) provisionOrgKeycloak(slug, manager string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	topPath := "/" + slug
	if err := s.kc.EnsureGroup(ctx, topPath); err != nil {
		return err
	}
	adminsID, err := s.kc.CreateSubGroup(ctx, topPath, "admins")
	if err != nil {
		return err
	}
	u, err := s.kc.GetUserByName(ctx, manager)
	if err != nil {
		return err
	}
	if err := s.kc.AddGroupMember(ctx, adminsID, u.ID); err != nil {
		return err
	}
	attrs, err := s.kc.GetUserAttributes(ctx, u.ID)
	if err != nil {
		return err
	}
	if attrs == nil {
		attrs = map[string][]string{}
	}
	attrs["org_admin"] = appendUnique(attrs["org_admin"], slug)
	return s.kc.SetUserAttributes(ctx, u.ID, attrs)
}

// cleanupOrgKeycloak best-effort removes the Keycloak groups for an org. It is
// only called when the DB write failed after Keycloak provisioning, so a retry
// does not leave orphan groups. Errors are logged, never fatal.
func (s *OrgService) cleanupOrgKeycloak(slug string) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if id, err := s.kc.GetGroupByName(ctx, slug+"/admins"); err == nil && id != "" {
		_ = s.kc.DeleteGroup(ctx, id)
	}
	if id, err := s.kc.GetGroupByName(ctx, slug); err == nil && id != "" {
		_ = s.kc.DeleteGroup(ctx, id)
	}
}

// EnsureDefaultOrgWithAdmin is the startup orchestration (ACCESS-CONTROL §2.2):
// it guarantees a default org exists with its `admins` subgroup and the given
// bootstrap admin user registered as org manager (L2 `org_admin`). Idempotent:
// an existing org is reconciled rather than recreated.
func (s *OrgService) EnsureDefaultOrgWithAdmin(ctx context.Context, adminUsername string) error {
	orgs, _, err := s.List(common.Pagination{Page: 1, PageSize: 1})
	if err != nil {
		return err
	}
	var def *models.Org
	if len(orgs) == 0 {
		def = models.DefaultOrg()
		if err := def.SetOrgManager(adminUsername); err != nil {
			return err
		}
		if err := s.Create(def); err != nil {
			return err
		}
	} else {
		def = &orgs[0]
	}
	if s.kc != nil {
		mgr := def.DecodeAnnotations().OrgManager
		if mgr == "" {
			mgr = adminUsername
			if err := def.SetOrgManager(mgr); err == nil {
				_ = s.orgRepo.Update(def)
			}
		}
		if err := s.provisionOrgKeycloak(def.Slug, mgr); err != nil {
			return err
		}
	}
	return nil
}

// MigrateOrgGroups reconciles the D1 carrier-group model (ACCESS-CONTROL §11):
// legacy `/org:<slug>` groups are converted to the hierarchical `/{slug}` +
// `/{slug}/admins` shape. Best-effort and idempotent; missing source groups
// are skipped. Called once at startup after EnsureDefaultOrgWithAdmin.
func (s *OrgService) MigrateOrgGroups(ctx context.Context) {
	if s.kc == nil {
		return
	}
	orgs, _, err := s.List(common.Pagination{Page: 1, PageSize: 500})
	if err != nil {
		logger.Warnf("org: migrate list failed: %v", err)
		return
	}
	for i := range orgs {
		slug := orgs[i].Slug
		legacy := "org:" + slug
		if id, err := s.kc.GetGroupByName(ctx, legacy); err == nil && id != "" {
			if err := s.provisionOrgKeycloak(slug, orgs[i].DecodeAnnotations().OrgManager); err != nil {
				logger.Warnf("org: migrate %s failed: %v", slug, err)
				continue
			}
			_ = s.kc.DeleteGroup(ctx, id)
		}
	}
}

// ReconcileGroups ensures every existing org already has its `/{slug}` carrier
// group. It is idempotent and best-effort, used for the one-time startup
// backfill of orgs created before group provisioning existed. Errors are
// logged, never fatal — a Keycloak outage must not block hub startup.
func (s *OrgService) ReconcileGroups(ctx context.Context) {
	if s.kc == nil {
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
			if err := s.kc.EnsureGroup(ctx, "/"+orgs[i].Slug); err != nil {
				logger.Warnf("org: reconcile group /%s failed: %v", orgs[i].Slug, err)
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
	// 与 ACCOUNT-PERMISSION-MODEL §11 步骤 4 的平台守卫收口一致。故此处不重复鉴权。
	//
	// 删除语义（DELETE-CONTRACT §6.4 表格 #1）：
	//   - Org 只做**软删除**（common.Base 含 gorm.DeletedAt，GORM 自动置 deleted_at，
	//     保留恢复窗口），**不**立刻级联物理清除下属资源；
	//   - **不**对 service 做级联（软删或硬删）：物理资源清空走独立运维流程，org
	//     删除保持轻量、可恢复（以 §6.4 更具体的表格为准，详见 internal/cascade 包注释）。
	//   - 若确需清掉某 org 的全部资源，调用方应在 **service 层**走
	//     cascade.Deleter.DeleteServiceSubtree（service→component→artifacts
	//     同事务级联软删 + 制品 GC 标记，并含"无活跃运行"事务内守卫），而非经 org 删除。
	//   - 级联清理 hub RoleBinding（platform/component 两类）+ 活跃 session 见
	//     internal/cascade（坑 2，ACCESS-CONTROL §10）。
	return s.orgRepo.Delete(id)
}

func (s *OrgService) GetServiceTree(orgID uuid.UUID) (*models.ServiceTree, error) {
	return s.serviceTreeRepo.GetByOrgID(orgID)
}

// appendUnique appends v to s unless already present.
func appendUnique(s []string, v string) []string {
	for _, x := range s {
		if x == v {
			return s
		}
	}
	return append(s, v)
}
