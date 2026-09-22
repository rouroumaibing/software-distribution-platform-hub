package service

import (
	"github.com/google/uuid"

	"github.com/rouroumaibing/software-distribution-platform-hub/internal/common"
	"github.com/rouroumaibing/software-distribution-platform-hub/internal/credentials/models"
	"github.com/rouroumaibing/software-distribution-platform-hub/internal/credentials/repository"
)

type CredentialService struct {
	repo *repository.CredentialRepository
}

func NewCredentialService(repo *repository.CredentialRepository) *CredentialService {
	return &CredentialService{repo: repo}
}

func (s *CredentialService) Create(in *models.Credential) error           { return s.repo.Create(in) }
func (s *CredentialService) Get(id uuid.UUID) (*models.Credential, error) { return s.repo.GetByID(id) }
func (s *CredentialService) Update(id uuid.UUID, in *models.Credential) error {
	in.ID = id
	return s.repo.Update(in)
}
func (s *CredentialService) Delete(id uuid.UUID) error { return s.repo.Delete(id) }
func (s *CredentialService) ListByScope(scope, scopeID string, p common.Pagination) ([]models.Credential, int64, error) {
	return s.repo.ListByScope(scope, scopeID, p)
}
