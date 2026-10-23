package service

import (
	"crypto/rand"
	"encoding/hex"
	"errors"

	"github.com/rouroumaibing/software-distribution-platform-hub/internal/common"
	"github.com/rouroumaibing/software-distribution-platform-hub/internal/target/models"
)

// ErrEnrollTokenInvalid reports a one-time enroll token that cannot be
// consumed (unknown / expired / already burned / name mismatch). The gateway
// answers every failure with a uniform 401.
var ErrEnrollTokenInvalid = errors.New("enroll token invalid or already used")

// ConsumeEnrollToken trades a one-time enroll token (issued by
// GenerateEnrollToken, carried in the join bundle) for a per-target gateway
// credential — the hub-side half of WS state ② in
// STEELMAN-ADR2-LEFTOVERS §7. Single-use and expiry are enforced atomically
// in the repository's conditional UPDATE; expectName, when non-empty,
// asserts the token is being presented under its own target's name.
func (s *TargetService) ConsumeEnrollToken(token, expectName string) (*models.Target, string, error) {
	if token == "" {
		return nil, "", ErrEnrollTokenInvalid
	}
	cred, err := newRunnerCredential()
	if err != nil {
		return nil, "", err
	}
	tg, err := s.repo.BurnEnrollToken(token, cred, expectName)
	if err != nil {
		return nil, "", ErrEnrollTokenInvalid
	}
	return tg, cred, nil
}

// GetByCredential resolves a target by its per-target gateway credential —
// the identity source for WS state ③ dials (§7); X-Target-Name is only a
// consistency assertion at the caller.
func (s *TargetService) GetByCredential(cred string) (*models.Target, error) {
	if cred == "" {
		return nil, ErrEnrollTokenInvalid
	}
	return s.repo.GetByCredential(cred)
}

// newRunnerCredential mints a 256-bit per-target bearer credential.
func newRunnerCredential() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", common.ErrInternal.WithError(err)
	}
	return hex.EncodeToString(buf), nil
}
