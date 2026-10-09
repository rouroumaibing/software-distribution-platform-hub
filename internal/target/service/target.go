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

// Enroll implements bootstrap-on-first-connect (ADR-0001): get-or-create a
// target from Runner-supplied registry attributes, so installing a Runner
// with its对接信息 completes the hookup without a manual console step —
// GitLab-Runner-style enrollment. The caller (gateway.ServeWS) must have
// authenticated the Runner with the gateway token first; Enroll itself
// performs no authorization. A create that loses the uniqueIndex race
// re-reads and returns the winner.
func (s *TargetService) Enroll(name, vendor, region string) (*models.Target, error) {
	if tg, err := s.repo.GetByName(name); err == nil {
		return tg, nil
	}
	tg := &models.Target{
		Name:       name,
		Vendor:     vendor,
		Region:     region,
		TargetKind: models.TargetKindK8s,
		Status:     models.TargetStatusOffline,
	}
	if err := s.repo.Create(tg); err != nil {
		// Lost a concurrent-create race on the unique name index: the
		// winner is the target row we hand back.
		if again, gerr := s.repo.GetByName(name); gerr == nil {
			return again, nil
		}
		return nil, err
	}
	return tg, nil
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

// RecordAgentInfo persists the runner's self-reported identity
// (RUNNER-REFLUX-SPEC §5): agent_version from the WS handshake frame plus
// last-seen. Not a user-facing CRUD op — gateway-only, like Heartbeat.
// A version change on an upgrade is how the hub reconciles the upgrade
// agent_ops row to its succeeded terminal state.
func (s *TargetService) RecordAgentInfo(id uuid.UUID, agentVersion string) error {
	tg, err := s.repo.GetByID(id)
	if err != nil {
		return err
	}
	if agentVersion != "" {
		tg.AgentVersion = agentVersion
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
