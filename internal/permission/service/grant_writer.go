package service

import (
	"context"

	"github.com/google/uuid"

	"github.com/rouroumaibing/software-distribution-platform-hub/internal/common"
	"github.com/rouroumaibing/software-distribution-platform-hub/internal/permission/models"
	"github.com/rouroumaibing/software-distribution-platform-hub/internal/permission/repository"
)

// GrantWriterImpl is the concrete GrantWriter used by the approval flow: on
// approval it writes the matching binding (platform vs component) keyed on the
// requester's `sub`, carrying the request's expires_at (ACCOUNT-PERMISSION-
// MODEL §7.4). It lives here (not in main) so it can reuse the service
// package's imports and stay unit-testable.
type GrantWriterImpl struct {
	bindingRepo      *repository.BindingRepository
	platformBindRepo *repository.PlatformRoleBindingRepository
}

func NewGrantWriter(
	bindingRepo *repository.BindingRepository,
	platformBindRepo *repository.PlatformRoleBindingRepository,
) *GrantWriterImpl {
	return &GrantWriterImpl{bindingRepo: bindingRepo, platformBindRepo: platformBindRepo}
}

func (w *GrantWriterImpl) GrantFromRequest(_ context.Context, req models.PermissionRequest) error {
	roleID := req.RequestedRoleID
	if req.RequestedRoleKind == "component" {
		if req.RequestedResourceID == uuid.Nil {
			return common.ErrBadRequest.WithMessage("component grant needs requestedResourceId")
		}
		return w.bindingRepo.Create(&models.ComponentRoleBinding{
			ComponentID:     req.RequestedResourceID,
			SubjectType:     "user",
			SubjectID:       req.Requester,
			ComponentRoleID: &roleID,
			ExpiresAt:       req.ExpiresAt,
		})
	}
	// Default platform grant (RequestedRoleKind == "platform").
	return w.platformBindRepo.Create(&models.PlatformRoleBinding{
		SubjectType:    "user",
		SubjectID:      req.Requester,
		PlatformRoleID: roleID,
		ExpiresAt:      req.ExpiresAt,
	})
}
