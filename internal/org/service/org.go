package service

import (
	"github.com/google/uuid"

	"github.com/rouroumaibing/software-distribution-platform-hub/internal/common"
	"github.com/rouroumaibing/software-distribution-platform-hub/internal/common/logger"
	"github.com/rouroumaibing/software-distribution-platform-hub/internal/org/models"
	"github.com/rouroumaibing/software-distribution-platform-hub/internal/org/repository"
)

// OrgService satisfies common.CRUDService[models.Org].
type OrgService struct {
	orgRepo         *repository.OrgRepository
	serviceTreeRepo *repository.ServiceTreeRepository
}

func NewOrgService(orgRepo *repository.OrgRepository, serviceTreeRepo *repository.ServiceTreeRepository) *OrgService {
	return &OrgService{orgRepo: orgRepo, serviceTreeRepo: serviceTreeRepo}
}

// Create creates an Org and its 1:1 ServiceTree together, so callers never
// have to remember to create the tree separately.
func (s *OrgService) Create(o *models.Org) error {
	if err := s.orgRepo.Create(o); err != nil {
		return err
	}
	return s.serviceTreeRepo.Create(&models.ServiceTree{OrgID: o.ID, Name: "default"})
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
