package models

import (
	"time"

	"github.com/google/uuid"

	"github.com/rouroumaibing/software-distribution-platform-hub/internal/common"
)

// Credential stores a secret reference / value used by direct-connect access
// (kubeconfig, ssh key/password). The plaintext Value is persisted but NEVER
// serialized to the client (json:"-"); read APIs return ValueSet instead
// (§7.12.4 desensitization).
type Credential struct {
	common.Base
	Name    string `gorm:"size:128;not null" json:"name"`
	Type    string `gorm:"size:32;not null" json:"type"`   // kubeconfig | ssh-key | ssh-password | basic
	Scope   string `gorm:"size:32" json:"scope,omitempty"` // target | environment
	ScopeID string `gorm:"size:64" json:"scopeId,omitempty"`

	// Value holds the secret plaintext. Persisted, but json:"-" keeps it out
	// of every response. Physical storage location (K8s Secret / encrypted
	// column / Vault) is a §9.7 未定项; this column is the interim store.
	Value    string `gorm:"type:text" json:"-"`
	ValueSet bool   `gorm:"-" json:"valueSet"`
}

func (Credential) TableName() string { return "credentials" }

const (
	CredTypeKubeconfig  = "kubeconfig"
	CredTypeSSHKey      = "ssh-key"
	CredTypeSSHPassword = "ssh-password"
	CredTypeBasic       = "basic"
)

// CredentialDTO is the desensitized view returned by every read API.
type CredentialDTO struct {
	ID        uuid.UUID `json:"id"`
	Name      string    `json:"name"`
	Type      string    `json:"type"`
	Scope     string    `json:"scope,omitempty"`
	ScopeID   string    `json:"scopeId,omitempty"`
	ValueSet  bool      `json:"valueSet"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

func (c *Credential) ToDTO() CredentialDTO {
	return CredentialDTO{
		ID:        c.ID,
		Name:      c.Name,
		Type:      c.Type,
		Scope:     c.Scope,
		ScopeID:   c.ScopeID,
		ValueSet:  c.Value != "",
		CreatedAt: c.CreatedAt,
		UpdatedAt: c.UpdatedAt,
	}
}

// CredentialInput is the create/update body. Value is accepted but never echoed.
type CredentialInput struct {
	Name    string `json:"name" binding:"required"`
	Type    string `json:"type" binding:"required"`
	Scope   string `json:"scope,omitempty"`
	ScopeID string `json:"scopeId,omitempty"`
	Value   string `json:"value"`
}
