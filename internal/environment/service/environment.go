package service

import (
	"github.com/google/uuid"

	"github.com/rouroumaibing/software-distribution-platform-hub/internal/common"
	"github.com/rouroumaibing/software-distribution-platform-hub/internal/environment/models"
	"github.com/rouroumaibing/software-distribution-platform-hub/internal/environment/repository"
)

// EnvironmentService satisfies common.CRUDService[models.Environment].
type EnvironmentService struct {
	repo *repository.EnvironmentRepository
}

func NewEnvironmentService(repo *repository.EnvironmentRepository) *EnvironmentService {
	return &EnvironmentService{repo: repo}
}

func (s *EnvironmentService) Create(e *models.Environment) error {
	// TODO: 如果 EnvType == production,创建时可以在这里触发一次策略检查
	// (比如要求关联的 Cluster 必须已经 online)。
	return s.repo.Create(e)
}

func (s *EnvironmentService) Get(id uuid.UUID) (*models.Environment, error) {
	return s.repo.GetByID(id)
}

func (s *EnvironmentService) ListByComponent(componentID uuid.UUID, p common.Pagination) ([]models.Environment, int64, error) {
	return s.repo.FindByComponentID(componentID, p)
}

func (s *EnvironmentService) Update(id uuid.UUID, e *models.Environment) error {
	e.ID = id
	return s.repo.Update(e)
}

func (s *EnvironmentService) Delete(id uuid.UUID) error {
	// TODO: 生产环境删除前应该二次确认,并检查该环境是否还有关联的运行中发布。
	return s.repo.Delete(id)
}
