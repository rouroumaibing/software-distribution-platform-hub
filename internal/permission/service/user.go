package service

import (
	"errors"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/rouroumaibing/software-distribution-platform-hub/internal/common"
	"github.com/rouroumaibing/software-distribution-platform-hub/internal/permission/models"
	"github.com/rouroumaibing/software-distribution-platform-hub/internal/permission/repository"
)

// UserService satisfies common.CRUDService[models.User].
type UserService struct{ repo *repository.UserRepository }

func NewUserService(repo *repository.UserRepository) *UserService { return &UserService{repo: repo} }

func (s *UserService) Create(u *models.User) error                            { return s.repo.Create(u) }
func (s *UserService) Get(id uuid.UUID) (*models.User, error)                 { return s.repo.GetByID(id) }
func (s *UserService) List(p common.Pagination) ([]models.User, int64, error) { return s.repo.List(p) }
func (s *UserService) Update(id uuid.UUID, u *models.User) error {
	u.ID = id
	return s.repo.Update(u)
}
func (s *UserService) Delete(id uuid.UUID) error { return s.repo.Delete(id) }

// GetOrProvisionByKeycloakID is called by AuthMiddleware on every request.
// The first time a valid token from a given Keycloak user arrives, this
// creates the local User row (JIT provisioning) — nobody has to run a
// manual "create account" step. OrgID is left empty here; assigning a user
// to an org happens through a separate invite/onboarding flow, not at
// first login.
func (s *UserService) GetOrProvisionByKeycloakID(keycloakID, email, name string) (*models.User, error) {
	u, err := s.repo.FindByKeycloakID(keycloakID)
	if err == nil {
		return u, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}

	newUser := &models.User{KeycloakID: keycloakID, Email: email, Name: name}
	if err := s.repo.Create(newUser); err != nil {
		return nil, err
	}
	return newUser, nil
}
