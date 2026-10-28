package models

import (
	"time"

	"github.com/rouroumaibing/software-distribution-platform-hub/internal/common"
)

// Target is an entry in the multi-vendor, multi-region target registry;
// a matching runner Agent connects back to the hub for each row here.
// TargetKind discriminates k8s clusters from non-container hosts (§9.5/§9.7).
type Target struct {
	common.BaseNoSoftDelete
	Name       string `gorm:"size:128;not null;uniqueIndex" json:"name"`
	Vendor     string `gorm:"size:64;not null" json:"vendor"`
	Region     string `gorm:"size:64;not null" json:"region"`
	TargetKind string `gorm:"size:16;not null;default:k8s" json:"targetKind"`
	Status     string `gorm:"size:32;not null;default:offline" json:"status"`

	AgentVersion    string     `gorm:"size:32" json:"agentVersion,omitempty"`
	LastHeartbeatAt *time.Time `json:"lastHeartbeatAt,omitempty"`

	// EnrollToken is a one-time bootstrap token returned by
	// POST /targets/:id/enroll-token; it is never serialized to the client.
	EnrollToken         *string    `gorm:"size:128" json:"-"`
	EnrollTokenExpireAt *time.Time `json:"-"`

	// RunnerCredential is the per-target bearer credential a Runner presents
	// on every gateway WS dial after trading in its one-time enroll token
	// (STEELMAN-ADR2-LEFTOVERS §7): the identity source on the wire, with
	// X-Target-Name demoted to a consistency assertion. Never serialized.
	RunnerCredential *string `gorm:"size:128;index" json:"-"`

	// AccessMode is how the hub/console manages this target (O-40, F2.1 统一
	// 纳管面). agent = 经 Runner Agent 反向连接（默认，兼容既有通道）；
	// kubeconfig / ssh = 经 credentialRef 引用的凭据直连，Agent 不在场。
	// agent 路径对 AccessMode 透明：agent_op.go 只认 agent，不读该字段。
	AccessMode string `gorm:"size:16;not null;default:agent" json:"accessMode"`

	// CredentialRef points at the external credential backing a non-agent
	// target (kubeconfig/ssh). Agents leave it nil. It is an opaque reference
	// (e.g. a credentials-service id); existence is validated upstream when
	// the credential store lands — here we only require it non-empty for
	// kubeconfig/ssh modes.
	CredentialRef *string `gorm:"size:255" json:"credentialRef,omitempty"`
}

func (Target) TableName() string { return "targets" }

const (
	TargetStatusOnline  = "online"
	TargetStatusOffline = "offline"

	TargetKindK8s  = "k8s"
	TargetKindHost = "host"

	// AccessMode* enumerate the O-40 target management channels.
	TargetAccessModeAgent      = "agent"
	TargetAccessModeKubeconfig = "kubeconfig"
	TargetAccessModeSSH        = "ssh"
)
