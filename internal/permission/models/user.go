package models

import (
	"github.com/google/uuid"

	"github.com/rouroumaibing/software-distribution-platform-hub/internal/common"
)

type User struct {
	common.Base
	OrgID uuid.UUID `gorm:"type:uuid;not null;index" json:"orgId"`
	Email string    `gorm:"size:256;not null" json:"email"`
	Name  string    `gorm:"size:128;not null" json:"name"`

	// KeycloakID is the token's "sub" claim — the stable, unique identity
	// Keycloak assigns per user. Auth middleware looks users up by this,
	// never by email (email can change; sub doesn't).
	KeycloakID string `gorm:"size:64;uniqueIndex" json:"-"`
}

func (User) TableName() string { return "users" }
