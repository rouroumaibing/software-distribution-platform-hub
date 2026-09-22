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
}

func (Target) TableName() string { return "targets" }

const (
	TargetStatusOnline  = "online"
	TargetStatusOffline = "offline"

	TargetKindK8s  = "k8s"
	TargetKindHost = "host"
)
