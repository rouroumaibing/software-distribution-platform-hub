package models

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"
)

// ResourceOwnership is the single authoritative source for "which org owns a
// resource" (ACCOUNT-PERMISSION-MODEL §3). The org_id column that was
// denormalized onto component_role_bindings is now a derived cache that this
// table rebuilds; the ownership decision in the auth middleware reads THIS
// table, never the cache.
//
// AllowedOrgs carries the "share with sibling orgs" set that a single
// owner_org column cannot express. It is stored as JSON so the membership
// test stays a single containment query.
type ResourceOwnership struct {
	ID           uuid.UUID      `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"id"`
	ResourceType string         `gorm:"size:32;not null;index" json:"resourceType"`
	ResourceID   uuid.UUID      `gorm:"type:uuid;not null;index" json:"resourceId"`
	OwnerOrg     uuid.UUID      `gorm:"type:uuid;index" json:"ownerOrg"`
	AllowedOrgs  datatypes.JSON `gorm:"type:jsonb" json:"allowedOrgs,omitempty"`
	CreatedAt    time.Time      `json:"createdAt"`
}

func (ResourceOwnership) TableName() string { return "resource_ownership" }

// SetAllowedOrgs serializes the share-with set into the JSON column. An empty
// slice is stored as "[]" (not NULL) so the membership test never has to
// special-case NULL.
func (o *ResourceOwnership) SetAllowedOrgs(orgs []uuid.UUID) error {
	if len(orgs) == 0 {
		o.AllowedOrgs = datatypes.JSON("[]")
		return nil
	}
	b, err := json.Marshal(orgs)
	if err != nil {
		return err
	}
	o.AllowedOrgs = datatypes.JSON(b)
	return nil
}

// AllowedOrgsSlice returns the parsed share-with set. A NULL/empty column is
// treated as "no extra orgs shared" (only OwnerOrg counts).
func (o *ResourceOwnership) AllowedOrgsSlice() []uuid.UUID {
	if len(o.AllowedOrgs) == 0 {
		return nil
	}
	var orgs []uuid.UUID
	if err := json.Unmarshal(o.AllowedOrgs, &orgs); err != nil {
		return nil
	}
	return orgs
}

// IsAllowed reports whether org may access the resource: it is the owner, or
// it appears in the share-with set. Pure logic, kept off the DB so the
// ownership decision is unit-testable without Postgres.
func (o *ResourceOwnership) IsAllowed(org uuid.UUID) bool {
	if org == uuid.Nil {
		return false
	}
	if o.OwnerOrg == org {
		return true
	}
	for _, a := range o.AllowedOrgsSlice() {
		if a == org {
			return true
		}
	}
	return false
}
