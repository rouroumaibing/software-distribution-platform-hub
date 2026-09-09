package service

import (
	"github.com/google/uuid"

	"github.com/rouroumaibing/software-distribution-platform-hub/internal/catalog/models"
	"github.com/rouroumaibing/software-distribution-platform-hub/internal/catalog/repository"
	"github.com/rouroumaibing/software-distribution-platform-hub/internal/common"
)

// ServiceService satisfies common.CRUDService[models.Service].
type ServiceService struct{ repo *repository.ServiceRepository }

func NewServiceService(repo *repository.ServiceRepository) *ServiceService {
	return &ServiceService{repo: repo}
}

func (s *ServiceService) Create(svc *models.Service) error { return s.repo.Create(svc) }

func (s *ServiceService) Get(id uuid.UUID) (*models.Service, error) { return s.repo.GetByID(id) }

func (s *ServiceService) ListByServiceTree(treeID uuid.UUID, p common.Pagination) ([]models.Service, int64, error) {
	return s.repo.FindByServiceTreeID(treeID, p)
}

func (s *ServiceService) Update(id uuid.UUID, svc *models.Service) error {
	svc.ID = id
	return s.repo.Update(svc)
}

func (s *ServiceService) Delete(id uuid.UUID) error {
	// TODO: 删除前检查是否还有下属 Component,避免误删导致孤儿数据。
	return s.repo.Delete(id)
}
