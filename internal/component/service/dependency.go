package service

import (
	"fmt"
	"net/http"

	"github.com/google/uuid"

	"github.com/rouroumaibing/software-distribution-platform-hub/internal/common"
	"github.com/rouroumaibing/software-distribution-platform-hub/internal/component/models"
	"github.com/rouroumaibing/software-distribution-platform-hub/internal/component/repository"
)

// DependencyService manages the §6.1 依赖图: add/remove edges with cycle
// rejection, list in both directions.
type DependencyService struct {
	repo       *repository.DependencyRepository
	components *ComponentService
}

func NewDependencyService(repo *repository.DependencyRepository, components *ComponentService) *DependencyService {
	return &DependencyService{repo: repo, components: components}
}

// List returns edges in the requested direction.
func (s *DependencyService) List(componentID uuid.UUID, upstream bool) ([]models.ComponentDependency, error) {
	return s.repo.ListByComponent(componentID, upstream)
}

// Add records component → dependsOn. Rejected: self-edge, unknown component,
// duplicate edge, and any edge that would close a cycle (DFS over the graph).
func (s *DependencyService) Add(componentID, dependsOnID uuid.UUID) (*models.ComponentDependency, error) {
	if componentID == dependsOnID {
		return nil, common.DomainErrorWithReasons(common.KindComponent, http.StatusBadRequest, 1,
			"self dependency", "组件不能依赖自身")
	}
	if _, err := s.components.Get(componentID); err != nil {
		return nil, err
	}
	if _, err := s.components.Get(dependsOnID); err != nil {
		return nil, fmt.Errorf("被依赖组件不存在: %w", err)
	}
	if dup, err := s.repo.Exists(componentID, dependsOnID); err == nil && dup {
		return nil, common.DomainErrorWithReasons(common.KindComponent, http.StatusConflict, 2,
			"duplicate dependency", "该依赖关系已存在")
	}
	all, err := s.repo.All()
	if err != nil {
		return nil, err
	}
	if closesCycle(all, componentID, dependsOnID) {
		return nil, common.DomainErrorWithReasons(common.KindComponent, http.StatusConflict, 3,
			"circular dependency",
			fmt.Sprintf("加入 %s → %s 会形成环状依赖，已被拒绝", short(componentID), short(dependsOnID)))
	}
	edge := &models.ComponentDependency{ComponentID: componentID, DependsOnID: dependsOnID}
	if err := s.repo.DB().Create(edge).Error; err != nil {
		return nil, err
	}
	return edge, nil
}

func (s *DependencyService) Remove(componentID, dependsOnID uuid.UUID) error {
	return s.repo.Delete(componentID, dependsOnID)
}

// closesCycle reports whether adding edge from→to closes a cycle, i.e. whether
// `to` can already reach `from` through existing edges.
func closesCycle(edges []models.ComponentDependency, from, to uuid.UUID) bool {
	adj := map[uuid.UUID][]uuid.UUID{}
	for _, e := range edges {
		adj[e.ComponentID] = append(adj[e.ComponentID], e.DependsOnID)
	}
	seen := map[uuid.UUID]bool{to: true}
	stack := []uuid.UUID{to}
	for len(stack) > 0 {
		cur := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if cur == from {
			return true
		}
		for _, next := range adj[cur] {
			if !seen[next] {
				seen[next] = true
				stack = append(stack, next)
			}
		}
	}
	return false
}

func short(id uuid.UUID) string { return id.String()[:8] }
