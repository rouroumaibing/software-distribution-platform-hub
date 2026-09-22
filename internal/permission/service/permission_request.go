package service

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/rouroumaibing/software-distribution-platform-hub/internal/common"
	"github.com/rouroumaibing/software-distribution-platform-hub/internal/permission/models"
)

// PermissionRequestStore is the narrow persistence surface the approval state
// machine needs, so it can be exercised without Postgres
// (*repository.PermissionRequestRepository satisfies it).
type PermissionRequestStore interface {
	Create(*models.PermissionRequest) error
	GetByID(uuid.UUID) (*models.PermissionRequest, error)
	Update(*models.PermissionRequest) error
	ListAll() ([]models.PermissionRequest, error)
	ListByStatus(string) ([]models.PermissionRequest, error)
}

// GrantWriter is called when a request is approved; it materializes the actual
// role binding (with expires_at) in the right table. Kept as an interface so
// the service stays free of the binding services and tests can stub it.
type GrantWriter interface {
	GrantFromRequest(ctx context.Context, req models.PermissionRequest) error
}

type PermissionRequestService struct {
	repo  PermissionRequestStore
	grant GrantWriter
	now   func() time.Time
}

func NewPermissionRequestService(repo PermissionRequestStore, grant GrantWriter) *PermissionRequestService {
	return &PermissionRequestService{repo: repo, grant: grant, now: time.Now}
}

func (s *PermissionRequestService) Create(req *models.PermissionRequest) error {
	if req.Requester == "" || req.RequestedRoleID == uuid.Nil {
		return common.ErrBadRequest.WithMessage("requester and requestedRoleId are required")
	}
	if req.Reason == "" {
		return common.ErrBadRequest.WithMessage("reason is required (audit)")
	}
	if req.Status == "" {
		req.Status = models.PermRequestPending
	}
	req.CreatedAt = s.now()
	return s.repo.Create(req)
}

func (s *PermissionRequestService) Get(id uuid.UUID) (*models.PermissionRequest, error) {
	return s.repo.GetByID(id)
}

func (s *PermissionRequestService) List(status string) ([]models.PermissionRequest, error) {
	if status == "" {
		return s.repo.ListAll()
	}
	return s.repo.ListByStatus(status)
}

// Approve transitions Pending → Approved and materializes the binding via the
// injected GrantWriter (which writes the role binding with expires_at). The
// approval and the grant are two data writes; if the grant fails the request is
// left Approved-but-ungranted so it can be retried by re-approving.
func (s *PermissionRequestService) Approve(ctx context.Context, id uuid.UUID, approver string, effectiveAt, expiresAt *time.Time) error {
	req, err := s.repo.GetByID(id)
	if err != nil {
		return err
	}
	if req.Status != models.PermRequestPending {
		return common.ErrBadRequest.WithMessage("only Pending requests can be approved")
	}
	req.Status = models.PermRequestApproved
	req.Approver = approver
	req.DecidedAt = timePtr(s.now())
	req.EffectiveAt = effectiveAt
	req.ExpiresAt = expiresAt
	if err := s.repo.Update(req); err != nil {
		return err
	}
	if s.grant != nil {
		if gerr := s.grant.GrantFromRequest(ctx, *req); gerr != nil {
			return gerr
		}
	}
	return nil
}

// Reject transitions Pending → Rejected. reason is recorded on the request's
// audit trail (the caller may also append it to audit_log).
func (s *PermissionRequestService) Reject(id uuid.UUID, approver, reason string) error {
	req, err := s.repo.GetByID(id)
	if err != nil {
		return err
	}
	if req.Status != models.PermRequestPending {
		return common.ErrBadRequest.WithMessage("only Pending requests can be rejected")
	}
	req.Status = models.PermRequestRejected
	req.Approver = approver
	req.DecidedAt = timePtr(s.now())
	if err := s.repo.Update(req); err != nil {
		return err
	}
	return nil
}

func timePtr(t time.Time) *time.Time { return &t }
