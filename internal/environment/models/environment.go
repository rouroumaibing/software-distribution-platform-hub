package models

import (
	"time"

	"github.com/google/uuid"

	"github.com/rouroumaibing/software-distribution-platform-hub/internal/common"
)

// Environment hangs off a Component (beta/alpha/gamma/prod) and pins it to
// a specific target + namespace. The access method (agent / kubeconfig / ssh)
// and its non-secret configuration live in Access / AccessConfig.
type Environment struct {
	common.BaseNoSoftDelete
	ComponentID uuid.UUID `gorm:"type:uuid;not null;index" json:"componentId"`
	Key         string    `gorm:"size:64;not null" json:"key"`
	Name        string    `gorm:"size:128;not null" json:"name"`
	TargetID    uuid.UUID `gorm:"type:uuid;not null;index" json:"targetId"`

	// EnvType is "test" or "production"; used for platform-level policy
	// checks (e.g. mandatory approval before production deploys)
	// independent of what the pipeline author configured.
	EnvType string `gorm:"size:16;not null;default:test" json:"envType"`

	// Namespace convention: {org}-{component}-{env}.
	Namespace string `gorm:"size:128;not null" json:"namespace"`

	// Access is the接入方式: agent (recommended, runner outbound) |
	// kubeconfig (hub direct to apiserver) | ssh (non-container host).
	Access string `gorm:"size:16;not null;default:agent" json:"access"`

	// GroupID points at an environment_groups row. Nullable: "ungrouped" is
	// a valid state (DATA-MODEL §8).
	GroupID *uuid.UUID `gorm:"type:uuid;index" json:"groupId,omitempty"`

	// AccessConfig holds access-specific, non-secret settings (credential refs
	// + echoed non-sensitive fields). Plaintext secrets live in the credentials
	// table, referenced by KubeCredRef / SSHSecretRef — never here (§9.7).
	AccessConfig EnvAccessConfig `gorm:"serializer:json;type:text" json:"accessConfig,omitempty"`

	// Status is the环境状态机 (§7.12.6): unconfigured | configured_unverified |
	// verified | failed. size:32 — "configured_unverified" is 21 chars; size:16
	// silently truncated it (SQLSTATE 22001) on every key-field-change rollback.
	Status string `gorm:"size:32;not null;default:unconfigured" json:"status"`

	LastTestAt     *time.Time `json:"lastTestAt,omitempty"`
	LastTestResult string     `gorm:"type:text" json:"lastTestResult,omitempty"`
}

func (Environment) TableName() string { return "environments" }

const (
	EnvTypeTest       = "test"
	EnvTypeProduction = "production"

	EnvAccessAgent      = "agent"
	EnvAccessKubeconfig = "kubeconfig"
	EnvAccessSSH        = "ssh"

	EnvStatusUnconfigured         = "unconfigured"
	EnvStatusConfiguredUnverified = "configured_unverified"
	EnvStatusVerified             = "verified"
	EnvStatusFailed               = "failed"
)

// EnvAccessConfig is the (non-secret) per-access configuration persisted on the
// environment. Secret material is referenced, not stored: KubeCredRef /
// SSHSecretRef point at rows in the credentials table.
type EnvAccessConfig struct {
	// kubeconfig access (hub direct connect)
	KubeSource          string `json:"kubeSource,omitempty"`  // ref | paste | manual
	KubeCredRef         string `json:"kubeCredRef,omitempty"` // id of a kubeconfig credential
	KubeServer          string `json:"kubeServer,omitempty"`  // echoed apiserver address (non-secret)
	KubeInsecureSkipTLS bool   `json:"kubeInsecureSkipTLS,omitempty"`
	KubeCurrentContext  string `json:"kubeCurrentContext,omitempty"`
	KubeDefaultNS       string `json:"kubeDefaultNS,omitempty"`

	// ssh access (non-container host, hub direct connect)
	SSHSecretRef string     `json:"sshSecretRef,omitempty"` // id of an ssh credential
	SSHSudo      bool       `json:"sshSudo,omitempty"`
	SSHTargets   []SSHEntry `json:"sshTargets,omitempty"`
}

// SSHEntry is one host in an ssh-access environment.
type SSHEntry struct {
	Host      string `json:"host"`
	Port      int    `json:"port,omitempty"`
	User      string `json:"user"`
	AuthType  string `json:"authType,omitempty"` // password | key
	SecretRef string `json:"secretRef,omitempty"`
	Bastion   string `json:"bastion,omitempty"`
}
