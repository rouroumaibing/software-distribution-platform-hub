package models

import (
	"github.com/google/uuid"

	"github.com/rouroumaibing/software-distribution-platform-hub/internal/common"
)

// Component is an independently deployable unit, typically backed by a
// single git repository.
type Component struct {
	common.Base
	ServiceID     uuid.UUID `gorm:"type:uuid;not null;index" json:"serviceId"`
	Key           string    `gorm:"size:64;not null" json:"key"`
	Name          string    `gorm:"size:128;not null" json:"name"`
	RepoURL       string    `gorm:"size:512;not null" json:"repoUrl"`
	DefaultBranch string    `gorm:"size:128;not null;default:main" json:"defaultBranch"`
	RepoSecretRef string    `gorm:"size:128" json:"repoSecretRef,omitempty"`
	Language      string    `gorm:"size:64" json:"language"`
	Description   string    `json:"description"`

	// Ownership (DATA-MODEL §7.4): drives the default approver for pipeline
	// approvals. Exactly one of OwnerSub / OwnerGroup is set. Nullable so
	// existing rows migrate cleanly; set on component create.
	//
	// OwnerSub holds the Keycloak `sub` (§5.3), not a local row id: hub keeps
	// no user table (D3), so the owner is identified by the same stable subject
	// every binding keys on. The column was `owner_user uuid` before D3;
	// migration 0015 backfills it from users.keycloak_id and explicitly nulls
	// the rows that cannot be mapped (with a warning query).
	OwnerSub   *string `gorm:"size:128;index" json:"ownerSub,omitempty"`
	OwnerGroup *string `gorm:"size:128;index" json:"ownerGroup,omitempty"`
}

func (Component) TableName() string { return "components" }
