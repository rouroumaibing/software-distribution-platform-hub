package service

import (
	"github.com/google/uuid"

	"github.com/rouroumaibing/software-distribution-platform-hub/internal/common"
	"github.com/rouroumaibing/software-distribution-platform-hub/internal/credentials/codec"
	"github.com/rouroumaibing/software-distribution-platform-hub/internal/credentials/models"
	"github.com/rouroumaibing/software-distribution-platform-hub/internal/credentials/repository"
)

type CredentialService struct {
	repo *repository.CredentialRepository
}

func NewCredentialService(repo *repository.CredentialRepository) *CredentialService {
	return &CredentialService{repo: repo}
}

// Create encrypts the secret value at rest before persisting. With no
// encryption key configured (dev), the value is stored plaintext (codec's
// documented fallback) — no data is lost either way.
func (s *CredentialService) Create(in *models.Credential) error {
	enc, err := codec.Encrypt(in.Value)
	if err != nil {
		return err
	}
	in.Value = enc
	return s.repo.Create(in)
}

func (s *CredentialService) Get(id uuid.UUID) (*models.Credential, error) {
	c, err := s.repo.GetByID(id)
	if err != nil {
		return nil, err
	}
	if c != nil {
		// Best-effort decrypt: an undecryptable row (e.g. key rotated away)
		// is kept as-is rather than failing the whole read.
		if dec, derr := codec.Decrypt(c.Value); derr == nil {
			c.Value = dec
		}
	}
	return c, nil
}

// Update encrypts the incoming value. NOTE: an empty input value encrypts to
// empty and overwrites the stored secret — the caller is responsible for
// sending the full value on update (pre-existing contract; preserve-current-
// on-empty would need a read-before-write, out of scope here).
func (s *CredentialService) Update(id uuid.UUID, in *models.Credential) error {
	enc, err := codec.Encrypt(in.Value)
	if err != nil {
		return err
	}
	in.ID = id
	in.Value = enc
	return s.repo.Update(in)
}

func (s *CredentialService) Delete(id uuid.UUID) error { return s.repo.Delete(id) }

// ListByScope decrypts each row's value in memory so callers always see
// plaintext; the persisted column stays ciphertext.
func (s *CredentialService) ListByScope(scope, scopeID string, p common.Pagination) ([]models.Credential, int64, error) {
	items, total, err := s.repo.ListByScope(scope, scopeID, p)
	if err != nil {
		return nil, 0, err
	}
	for i := range items {
		if dec, derr := codec.Decrypt(items[i].Value); derr == nil {
			items[i].Value = dec
		}
	}
	return items, total, nil
}
