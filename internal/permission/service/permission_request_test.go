package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/rouroumaibing/software-distribution-platform-hub/internal/permission/models"
)

type fakeRequestStore struct {
	items map[uuid.UUID]*models.PermissionRequest
}

func newFakeRequestStore() *fakeRequestStore {
	return &fakeRequestStore{items: map[uuid.UUID]*models.PermissionRequest{}}
}

func (s *fakeRequestStore) Create(r *models.PermissionRequest) error {
	if r.ID == uuid.Nil {
		r.ID = uuid.New()
	}
	s.items[r.ID] = r
	return nil
}
func (s *fakeRequestStore) GetByID(id uuid.UUID) (*models.PermissionRequest, error) {
	if r, ok := s.items[id]; ok {
		return r, nil
	}
	return nil, gorm.ErrRecordNotFound
}
func (s *fakeRequestStore) Update(r *models.PermissionRequest) error { s.items[r.ID] = r; return nil }
func (s *fakeRequestStore) ListAll() ([]models.PermissionRequest, error) {
	var out []models.PermissionRequest
	for _, r := range s.items {
		out = append(out, *r)
	}
	return out, nil
}
func (s *fakeRequestStore) ListByStatus(status string) ([]models.PermissionRequest, error) {
	var out []models.PermissionRequest
	for _, r := range s.items {
		if r.Status == status {
			out = append(out, *r)
		}
	}
	return out, nil
}

type fakeGrantWriter struct {
	called int
	last   models.PermissionRequest
	err    error
}

func (g *fakeGrantWriter) GrantFromRequest(_ context.Context, r models.PermissionRequest) error {
	g.called++
	g.last = r
	return g.err
}

func newPending(t *testing.T, svc *PermissionRequestService) *models.PermissionRequest {
	t.Helper()
	req := &models.PermissionRequest{Requester: "sub-1", RequestedRoleID: uuid.New(), RequestedRoleKind: "platform", Reason: "need access"}
	if err := svc.Create(req); err != nil {
		t.Fatalf("Create: %v", err)
	}
	return req
}

func TestPermissionRequest_CreateRequiresReason(t *testing.T) {
	svc := NewPermissionRequestService(newFakeRequestStore(), &fakeGrantWriter{})
	if err := svc.Create(&models.PermissionRequest{Requester: "sub-1", RequestedRoleID: uuid.New()}); err == nil {
		t.Fatal("a request without a reason must be rejected (audit requirement)")
	}
}

func TestPermissionRequest_CreateRequiresRequesterAndRole(t *testing.T) {
	svc := NewPermissionRequestService(newFakeRequestStore(), &fakeGrantWriter{})
	if err := svc.Create(&models.PermissionRequest{RequestedRoleID: uuid.New(), Reason: "x"}); err == nil {
		t.Fatal("missing requester must be rejected")
	}
	if err := svc.Create(&models.PermissionRequest{Requester: "sub-1", Reason: "x"}); err == nil {
		t.Fatal("missing requestedRoleId must be rejected")
	}
}

func TestPermissionRequest_ApproveWritesBinding(t *testing.T) {
	grant := &fakeGrantWriter{}
	svc := NewPermissionRequestService(newFakeRequestStore(), grant)
	req := newPending(t, svc)

	exp := time.Now().Add(24 * time.Hour)
	if err := svc.Approve(context.Background(), req.ID, "admin-sub", nil, &exp); err != nil {
		t.Fatalf("Approve: %v", err)
	}
	if req.Status != models.PermRequestApproved {
		t.Errorf("status = %q, want Approved", req.Status)
	}
	if req.Approver != "admin-sub" || req.DecidedAt == nil {
		t.Errorf("approver/decidedAt not recorded: %+v", req)
	}
	if grant.called != 1 {
		t.Fatalf("grant writer called %d times, want 1", grant.called)
	}
	if grant.last.ExpiresAt == nil || !grant.last.ExpiresAt.Equal(exp) {
		t.Errorf("expires_at not propagated to the binding: %+v", grant.last.ExpiresAt)
	}
}

func TestPermissionRequest_CannotApproveTwice(t *testing.T) {
	grant := &fakeGrantWriter{}
	svc := NewPermissionRequestService(newFakeRequestStore(), grant)
	req := newPending(t, svc)

	if err := svc.Approve(context.Background(), req.ID, "admin", nil, nil); err != nil {
		t.Fatalf("first Approve: %v", err)
	}
	if err := svc.Approve(context.Background(), req.ID, "admin", nil, nil); err == nil {
		t.Fatal("a non-Pending request must not be approvable")
	}
	if grant.called != 1 {
		t.Errorf("grant writer must not run on the second approve: called=%d", grant.called)
	}
}

func TestPermissionRequest_RejectTransitions(t *testing.T) {
	svc := NewPermissionRequestService(newFakeRequestStore(), &fakeGrantWriter{})
	req := newPending(t, svc)
	if err := svc.Reject(req.ID, "admin", "not justified"); err != nil {
		t.Fatalf("Reject: %v", err)
	}
	if req.Status != models.PermRequestRejected || req.DecidedAt == nil {
		t.Errorf("reject not recorded: %+v", req)
	}
	if err := svc.Reject(req.ID, "admin", "again"); err == nil {
		t.Fatal("a non-Pending request must not be rejectable")
	}
}

// If the grant write fails, the approval must surface the error (the request is
// left Approved-but-ungranted so it can be retried) — a silent success would
// look like the subject got access when it did not.
func TestPermissionRequest_GrantFailureSurfaces(t *testing.T) {
	grant := &fakeGrantWriter{err: errors.New("binding store down")}
	svc := NewPermissionRequestService(newFakeRequestStore(), grant)
	req := newPending(t, svc)

	if err := svc.Approve(context.Background(), req.ID, "admin", nil, nil); err == nil {
		t.Fatal("a failed grant must not report success")
	}
}
