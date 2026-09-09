package service

import (
	"time"

	"github.com/google/uuid"

	"github.com/rouroumaibing/software-distribution-platform-hub/internal/cluster/models"
	"github.com/rouroumaibing/software-distribution-platform-hub/internal/cluster/repository"
	"github.com/rouroumaibing/software-distribution-platform-hub/internal/common"
)

// ClusterService satisfies common.CRUDService[models.Cluster].
type ClusterService struct{ repo *repository.ClusterRepository }

func NewClusterService(repo *repository.ClusterRepository) *ClusterService {
	return &ClusterService{repo: repo}
}

func (s *ClusterService) Create(cl *models.Cluster) error           { return s.repo.Create(cl) }
func (s *ClusterService) Get(id uuid.UUID) (*models.Cluster, error) { return s.repo.GetByID(id) }
func (s *ClusterService) List(p common.Pagination) ([]models.Cluster, int64, error) {
	return s.repo.List(p)
}
func (s *ClusterService) Update(id uuid.UUID, cl *models.Cluster) error {
	cl.ID = id
	return s.repo.Update(cl)
}
func (s *ClusterService) Delete(id uuid.UUID) error { return s.repo.Delete(id) }

// GetByName resolves a cluster by its unique Name — used by the gateway to
// map a Runner's X-Cluster-Name header onto a hub-side cluster row.
func (s *ClusterService) GetByName(name string) (*models.Cluster, error) {
	return s.repo.GetByName(name)
}

// Heartbeat is called by the gateway whenever a Runner Agent's connection
// is (re)established, or on each periodic keepalive — not a user-facing
// CRUD op, so it lives outside the standard Update() signature.
func (s *ClusterService) Heartbeat(id uuid.UUID, online bool) error {
	cl, err := s.repo.GetByID(id)
	if err != nil {
		return err
	}
	if online {
		cl.Status = models.ClusterStatusOnline
	} else {
		cl.Status = models.ClusterStatusOffline
	}
	now := time.Now()
	cl.LastHeartbeatAt = &now
	return s.repo.Update(cl)
}
