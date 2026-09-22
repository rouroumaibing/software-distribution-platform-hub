package service

import (
	"crypto/rand"
	"encoding/hex"
	"time"

	"github.com/google/uuid"

	"github.com/rouroumaibing/software-distribution-platform-hub/internal/common"
	"github.com/rouroumaibing/software-distribution-platform-hub/internal/target/models"
	"github.com/rouroumaibing/software-distribution-platform-hub/internal/target/repository"
)

// TargetService satisfies common.CRUDService[models.Target].
type TargetService struct{ repo *repository.TargetRepository }

func NewTargetService(repo *repository.TargetRepository) *TargetService {
	return &TargetService{repo: repo}
}

func (s *TargetService) Create(tg *models.Target) error           { return s.repo.Create(tg) }
func (s *TargetService) Get(id uuid.UUID) (*models.Target, error) { return s.repo.GetByID(id) }
func (s *TargetService) List(p common.Pagination) ([]models.Target, int64, error) {
	return s.repo.List(p)
}
func (s *TargetService) Update(id uuid.UUID, tg *models.Target) error {
	tg.ID = id
	return s.repo.Update(tg)
}
func (s *TargetService) Delete(id uuid.UUID) error { return s.repo.Delete(id) }

// GetByName resolves a target by its unique Name — used by the gateway to
// map a Runner's X-Target-Name header onto a hub-side target row.
func (s *TargetService) GetByName(name string) (*models.Target, error) {
	return s.repo.GetByName(name)
}

// Heartbeat is called by the gateway whenever a Runner Agent's connection
// is (re)established, or on each periodic keepalive — not a user-facing
// CRUD op, so it lives outside the standard Update() signature.
func (s *TargetService) Heartbeat(id uuid.UUID, online bool) error {
	tg, err := s.repo.GetByID(id)
	if err != nil {
		return err
	}
	if online {
		tg.Status = models.TargetStatusOnline
	} else {
		tg.Status = models.TargetStatusOffline
	}
	now := time.Now()
	tg.LastHeartbeatAt = &now
	return s.repo.Update(tg)
}

// GenerateEnrollToken issues a one-time bootstrap token for a target (§9.9):
// the console registers a target, then exchanges this token to enroll a Runner
// Agent. The plaintext token is returned exactly once; only its hash-equivalent
// reference is stored (here: the token itself, since it is single-use and
// short-lived).
func (s *TargetService) GenerateEnrollToken(id uuid.UUID) (string, error) {
	tg, err := s.repo.GetByID(id)
	if err != nil {
		return "", err
	}
	buf := make([]byte, 24)
	if _, err := rand.Read(buf); err != nil {
		return "", common.ErrInternal.WithError(err)
	}
	token := hex.EncodeToString(buf)
	now := time.Now()
	expire := now.Add(24 * time.Hour)
	tg.EnrollToken = &token
	tg.EnrollTokenExpireAt = &expire
	if err := s.repo.Update(tg); err != nil {
		return "", err
	}
	return token, nil
}
