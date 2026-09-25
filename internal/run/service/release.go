package service

import (
	"fmt"

	"github.com/google/uuid"

	runnerapi "github.com/rouroumaibing/software-distribution-platform-runner/api/v1alpha1"

	"github.com/rouroumaibing/software-distribution-platform-hub/internal/common"
	"github.com/rouroumaibing/software-distribution-platform-hub/internal/run/models"
	"github.com/rouroumaibing/software-distribution-platform-hub/internal/run/repository"
)

// ReleaseService exposes CRUD over progressive-delivery history (RolloutRun
// rows) so the console's release view can list / inspect / roll back published
// versions instead of aggregating from runs on the frontend (P0-1).
type ReleaseService struct {
	repo *repository.RolloutRunRepository
}

func NewReleaseService(repo *repository.RolloutRunRepository) *ReleaseService {
	return &ReleaseService{repo: repo}
}

func (s *ReleaseService) Create(m *models.RolloutRun) error { return s.repo.Create(m) }

func (s *ReleaseService) Get(id uuid.UUID) (*models.RolloutRun, error) { return s.repo.GetByID(id) }

// List supports the console release-view filters (STATUS §2 #14):
//   - pipelineRunID: 仅该运行的发布（原能力保留）
//   - state:         语义键 all/running/paused/succeeded → RolloutPhase
//     （releases 是全局资源，无组件作用域；"paused" 即 RolloutPhase=Paused）
func (s *ReleaseService) List(p common.Pagination, pipelineRunID *uuid.UUID, state string) ([]models.RolloutRun, int64, error) {
	phase, err := mapReleaseState(state)
	if err != nil {
		return nil, 0, err
	}
	return s.repo.List(p, pipelineRunID, phase)
}

// mapReleaseState 把发布视图的语义筛选键映射为 RolloutRun.Phase 值。空/"all"
// 表示不过滤。非法键返回错误，由 handler 转 400（不静默放过）。
func mapReleaseState(state string) (string, error) {
	switch state {
	case "", "all":
		return "", nil
	case "running": // 进行中
		return string(runnerapi.RolloutProgressing), nil
	case "paused": // 已暂停
		return string(runnerapi.RolloutPaused), nil
	case "succeeded": // 成功
		return string(runnerapi.RolloutHealthy), nil
	default:
		return "", fmt.Errorf("invalid release state %q: want one of all|running|paused|succeeded", state)
	}
}

// Update applies the provided fields onto the existing row (preserving the FK
// and any fields the caller left out) and persists.
func (s *ReleaseService) Update(id uuid.UUID, m *models.RolloutRun) error {
	existing, err := s.repo.GetByID(id)
	if err != nil {
		return err
	}
	m.ID = existing.ID
	return s.repo.Update(m)
}

func (s *ReleaseService) Delete(id uuid.UUID) error { return s.repo.Delete(id) }
