// Package auth holds cross-cutting authentication configuration the BFF session
// layer and the realm-settings admin surface share (AUTH-ARCHITECTURE §2).
//
// The protocol-layer login mode (default | keycloak, mutually exclusive, default
// default) is stored in the hub DB — not a k8s ConfigMap — so the hub can switch
// it at runtime through an admin endpoint without a kube client. The chart seeds
// the initial value via env (LOGIN_MODE); the value then lives in the DB.
package auth

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"gorm.io/gorm"

	"github.com/rouroumaibing/software-distribution-platform-hub/internal/platform"
)

const loginModeKey = "auth.loginMode"

// DefaultLoginMode is the mode used when no DB row exists yet (first boot).
const DefaultLoginMode = "default"

// LoginModeService reads/writes the protocol-layer login mode. The hub is the
// authority; Keycloak is only the identity store.
type LoginModeService interface {
	Get(ctx context.Context) (string, error)
	Set(ctx context.Context, mode string) error
}

// SettingLoginMode is a DB-backed LoginModeService with an in-memory cache so the
// hot /auth/config path never hits Postgres.
type SettingLoginMode struct {
	store platform.SettingStore
	seed  string
	mu    sync.RWMutex
	cache string
}

// NewSettingLoginMode builds the service. seed is used only when no row exists
// yet; an empty seed falls back to DefaultLoginMode.
func NewSettingLoginMode(store platform.SettingStore, seed string) *SettingLoginMode {
	if seed == "" {
		seed = DefaultLoginMode
	}
	return &SettingLoginMode{store: store, seed: seed}
}

// Get returns the cached mode, else the DB row, else the seed.
func (s *SettingLoginMode) Get(ctx context.Context) (string, error) {
	s.mu.RLock()
	if s.cache != "" {
		v := s.cache
		s.mu.RUnlock()
		return v, nil
	}
	s.mu.RUnlock()

	v, err := s.store.Get(loginModeKey)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return s.seed, nil
	}
	if err != nil {
		return "", err
	}
	if v == "" {
		v = s.seed
	}
	s.mu.Lock()
	s.cache = v
	s.mu.Unlock()
	return v, nil
}

// Set validates and persists the mode, refreshing the cache.
func (s *SettingLoginMode) Set(ctx context.Context, mode string) error {
	if mode != "default" && mode != "keycloak" {
		return fmt.Errorf("auth: invalid login mode %q (want default|keycloak)", mode)
	}
	if err := s.store.Put(loginModeKey, mode); err != nil {
		return err
	}
	s.mu.Lock()
	s.cache = mode
	s.mu.Unlock()
	return nil
}
