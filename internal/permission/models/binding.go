package models

import (
	"time"

	"github.com/google/uuid"
)

// ComponentRoleBinding grants a role to a subject on one Component.
// No binding means no access — there's no implicit default.
//
// Multi-org: OrgID is denormalized (resolved from Component → Service →
// ServiceTree → Org at create time) so RBAC lookups can be isolated per
// org without a join.
//
// §7 subject model:
//   - SubjectType/SubjectID : subject (`user` | `group`).
//   - ComponentRoleID       : the (component_roles) role granted.
//
// The V1 legacy columns UserID/RoleID are gone (D3, migration 0015). They
// pointed at the local `users` table and the V1 `roles` table; since hub no
// longer stores identities, a binding has exactly one way to name a subject —
// SubjectID holding the Keycloak `sub` (§5.3). The `roles` table itself is
// retained (its own removal was not part of the approved D3 scope).
type ComponentRoleBinding struct {
	ID              uuid.UUID  `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"id"`
	ComponentID     uuid.UUID  `gorm:"type:uuid;not null;index" json:"componentId"`
	OrgID           *uuid.UUID `gorm:"type:uuid;index" json:"orgId,omitempty"`
	SubjectType     string     `gorm:"size:16" json:"subjectType,omitempty"`             // user|group
	SubjectID       string     `gorm:"size:128" json:"subjectId,omitempty"`              // Keycloak sub | group name (§5.3)
	ComponentRoleID *uuid.UUID `gorm:"type:uuid;index" json:"componentRoleId,omitempty"` // §7 role
	// GrantedBy is the granting subject's `sub` (text since 0015 — it used to be
	// a local users.id uuid).
	GrantedBy *string   `gorm:"size:128" json:"grantedBy,omitempty"`
	GrantedAt time.Time `gorm:"not null;default:now()" json:"grantedAt"`
	// ExpiresAt is the grant's TTL (ACCOUNT-PERMISSION-MODEL §7.4). NULL =
	// permanent; a past non-NULL value means the grant is dead and
	// ListMatching filters it out, so an expired row is never a permission
	// source (§10 #15 与 platform_role_bindings 同批落地).
	ExpiresAt *time.Time `json:"expiresAt,omitempty"`
}

func (ComponentRoleBinding) TableName() string { return "component_role_bindings" }
