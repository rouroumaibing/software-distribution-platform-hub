package service

import (
	"fmt"
	"net/http"

	"github.com/google/uuid"

	"github.com/rouroumaibing/software-distribution-platform-hub/internal/common"
	"github.com/rouroumaibing/software-distribution-platform-hub/internal/environmentgroup/models"
)

// EnvironmentGroupStore is the persistence surface EnvironmentGroupService
// needs. An interface (same pattern as ComponentStore / StageStore) so the
// non-empty guard is unit-testable without Postgres;
// *repository.EnvironmentGroupRepository satisfies it as-is.
type EnvironmentGroupStore interface {
	Create(*models.EnvironmentGroup) error
	GetByID(uuid.UUID) (*models.EnvironmentGroup, error)
	Update(*models.EnvironmentGroup) error
	Delete(uuid.UUID) error
	FindByComponent(uuid.UUID, common.Pagination) ([]models.EnvironmentGroup, int64, error)
	CountEnvironments(uuid.UUID) (int64, error)
}

type EnvironmentGroupService struct {
	repo EnvironmentGroupStore
}

func NewEnvironmentGroupService(repo EnvironmentGroupStore) *EnvironmentGroupService {
	return &EnvironmentGroupService{repo: repo}
}

func (s *EnvironmentGroupService) Create(g *models.EnvironmentGroup) error { return s.repo.Create(g) }
func (s *EnvironmentGroupService) Get(id uuid.UUID) (*models.EnvironmentGroup, error) {
	return s.repo.GetByID(id)
}
func (s *EnvironmentGroupService) ListByComponent(componentID uuid.UUID, p common.Pagination) ([]models.EnvironmentGroup, int64, error) {
	return s.repo.FindByComponent(componentID, p)
}
func (s *EnvironmentGroupService) Update(id uuid.UUID, g *models.EnvironmentGroup) error {
	g.ID = id
	return s.repo.Update(g)
}

// Delete enforces the "empty groups only" rule (DELETE-CONTRACT §6.4 #11,
// DATA-MODEL §8.4): a group still holding environments must not be deleted,
// because group membership is user-authored structure and silently dropping it
// loses the classification. The rejection is `409 + {reasons}` — same verdict
// shape as every other delete guard in the contract — so the console can render
// the blocking reason without special-casing this endpoint.
func (s *EnvironmentGroupService) Delete(id uuid.UUID) error {
	n, err := s.repo.CountEnvironments(id)
	if err != nil {
		return err
	}
	if n > 0 {
		return common.DomainErrorWithReasons(common.KindEnv, http.StatusConflict, 2,
			"environment group is not empty; move or delete its environments first",
			fmt.Sprintf("该分组下仍有 %d 个环境，请先移出或删除后再删除分组", n))
	}
	return s.repo.Delete(id)
}
