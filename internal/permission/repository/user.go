package repository

import (
	"gorm.io/gorm"

	"github.com/rouroumaibing/software-distribution-platform-hub/internal/common"
	"github.com/rouroumaibing/software-distribution-platform-hub/internal/permission/models"
)

type UserRepository struct {
	*common.Repository[models.User]
}

func NewUserRepository(db *gorm.DB) *UserRepository {
	return &UserRepository{common.NewRepository[models.User](db)}
}

// FindByKeycloakID looks up a user by their Keycloak "sub" claim. Returns
// gorm.ErrRecordNotFound when no local user has been provisioned yet —
// the caller (AuthMiddleware's JIT provisioning) treats that as "create
// one now", not as an error to surface to the client.
func (r *UserRepository) FindByKeycloakID(keycloakID string) (*models.User, error) {
	var u models.User
	if err := r.DB.First(&u, "keycloak_id = ?", keycloakID).Error; err != nil {
		return nil, err
	}
	return &u, nil
}
