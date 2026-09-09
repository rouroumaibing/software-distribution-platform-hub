package repository

import (
	"gorm.io/gorm"

	"github.com/rouroumaibing/software-distribution-platform-hub/internal/cluster/models"
	"github.com/rouroumaibing/software-distribution-platform-hub/internal/common"
)

type ClusterRepository struct {
	*common.Repository[models.Cluster]
}

func NewClusterRepository(db *gorm.DB) *ClusterRepository {
	return &ClusterRepository{common.NewRepository[models.Cluster](db)}
}

// GetByName resolves a cluster by its unique Name (the identifier a Runner
// presents as X-Cluster-Name when it dials the gateway).
func (r *ClusterRepository) GetByName(name string) (*models.Cluster, error) {
	var cl models.Cluster
	if err := r.DB.First(&cl, "name = ?", name).Error; err != nil {
		return nil, err
	}
	return &cl, nil
}
