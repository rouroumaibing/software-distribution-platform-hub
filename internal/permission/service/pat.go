package service

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/rouroumaibing/software-distribution-platform-hub/internal/common"
	"github.com/rouroumaibing/software-distribution-platform-hub/internal/permission/models"
)

// PATPrefix is the wire-format prefix that routes a bearer token into the PAT
// verification path instead of Keycloak JWT verification.
const PATPrefix = "sdp_pat_"

// PATStore is the persistence slice PATService needs; *repository.PATRepository
// satisfies it and tests inject fakes.
type PATStore interface {
	Create(*models.PersonalAccessToken) error
	FindByHash(string) (*models.PersonalAccessToken, error)
	ListBySubject(string) ([]models.PersonalAccessToken, error)
	Revoke(string, uuid.UUID, time.Time) error
	TouchLastUsed(uuid.UUID, time.Time)
}

// PATService manages personal access tokens (§6.4). Token plaintext exists
// only at creation — the store keeps sha256(token) only (S-1).
type PATService struct {
	repo PATStore
	now  func() time.Time
}

func NewPATService(repo PATStore) *PATService {
	return &PATService{repo: repo, now: time.Now}
}

// CreateTokenInput is the POST /me/tokens body.
type CreateTokenInput struct {
	Name      string     `json:"name" binding:"required"`
	Scopes    string     `json:"scopes"` // read | write（默认 write）
	ExpiresAt *time.Time `json:"expiresAt"`
}

// Create generates a token, persists only its hash, and returns the plaintext
// exactly once (S-1：明文仅创建时展示).
func (s *PATService) Create(subject string, in CreateTokenInput) (*models.PersonalAccessToken, string, error) {
	if in.Scopes == "" {
		in.Scopes = models.PATScopeWrite
	}
	if in.Scopes != models.PATScopeRead && in.Scopes != models.PATScopeWrite {
		return nil, "", common.DomainErrorWithReasons(common.KindAuth, 400, 1,
			"invalid scope", "scopes 须为 read 或 write")
	}
	if len(in.Name) > 128 {
		return nil, "", common.DomainErrorWithReasons(common.KindAuth, 400, 2,
			"name too long", "令牌名称过长（≤128 字符）")
	}
	plaintext, err := generatePAT()
	if err != nil {
		return nil, "", err
	}
	sum := sha256.Sum256([]byte(plaintext))
	row := &models.PersonalAccessToken{
		Subject:   subject,
		Name:      in.Name,
		Prefix:    plaintext[:len(PATPrefix)+8],
		TokenHash: hex.EncodeToString(sum[:]),
		Scopes:    in.Scopes,
		ExpiresAt: in.ExpiresAt,
	}
	if err := s.repo.Create(row); err != nil {
		return nil, "", err
	}
	return row, plaintext, nil
}

// Verify resolves a presented PAT plaintext to its row. Returns an error for
// revoked / expired / unknown tokens (fail-closed on all three).
func (s *PATService) Verify(plaintext string) (*models.PersonalAccessToken, error) {
	sum := sha256.Sum256([]byte(plaintext))
	row, err := s.repo.FindByHash(hex.EncodeToString(sum[:]))
	if err != nil {
		return nil, fmt.Errorf("token 无效")
	}
	if row.RevokedAt != nil {
		return nil, fmt.Errorf("token 已吊销")
	}
	if row.ExpiresAt != nil && s.now().After(*row.ExpiresAt) {
		return nil, fmt.Errorf("token 已过期")
	}
	s.repo.TouchLastUsed(row.ID, s.now())
	return row, nil
}

// List returns the subject's active tokens (masked — TokenHash never leaves).
func (s *PATService) List(subject string) ([]models.PersonalAccessToken, error) {
	return s.repo.ListBySubject(subject)
}

// Revoke marks the token revoked; subject-scoped so you can only revoke your own.
func (s *PATService) Revoke(subject string, id uuid.UUID) error {
	if err := s.repo.Revoke(subject, id, s.now()); err != nil {
		return err
	}
	return nil
}

// ErrPATNotFound re-exposed for handler 404 mapping.
var ErrPATNotFound = gorm.ErrRecordNotFound

// generatePAT returns "sdp_pat_" + 32 hex chars (128-bit entropy).
func generatePAT() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate pat: %w", err)
	}
	return PATPrefix + hex.EncodeToString(b), nil
}
