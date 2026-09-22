package models

import (
	"time"

	"github.com/google/uuid"
)

// PermissionRequest is the approval-request side of the permission model
// (ACCOUNT-PERMISSION-MODEL §7). It is intentionally a separate flow from
// pipeline approvals (run/models/pipeline_approval.go): this one asks for a
// role grant, the other approves a pipeline gate. Approval writes a binding
// (with expires_at) via the permission services; the two modules meet only
// through data, never through a function call.
type PermissionRequest struct {
	ID                    uuid.UUID  `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"id"`
	Requester             string     `gorm:"size:128;not null;index" json:"requester"` // sub
	RequestedRoleID       uuid.UUID  `gorm:"type:uuid;not null;index" json:"requestedRoleId"`
	RequestedRoleKind     string     `gorm:"size:16;not null" json:"requestedRoleKind"`      // platform | component
	RequestedResourceType string     `gorm:"size:32" json:"requestedResourceType,omitempty"` // component | pipeline | nil=platform
	RequestedResourceID   uuid.UUID  `gorm:"type:uuid" json:"requestedResourceId,omitempty"`
	Reason                string     `gorm:"type:text;not null" json:"reason"`
	Approver              string     `gorm:"size:128" json:"approver,omitempty"`
	Status                string     `gorm:"size:16;not null;default:'Pending'" json:"status"` // Pending | Approved | Rejected
	EffectiveAt           *time.Time `json:"effectiveAt,omitempty"`
	ExpiresAt             *time.Time `json:"expiresAt,omitempty"`
	CreatedAt             time.Time  `json:"createdAt"`
	DecidedAt             *time.Time `json:"decidedAt,omitempty"`
}

func (PermissionRequest) TableName() string { return "permission_requests" }

// PermissionRequestStatus constants.
const (
	PermRequestPending  = "Pending"
	PermRequestApproved = "Approved"
	PermRequestRejected = "Rejected"
)
