package models

import (
	"time"

	"github.com/rouroumaibing/software-distribution-platform-hub/internal/common"
)

// Cluster is an entry in the multi-vendor, multi-region cluster registry;
// a matching runner Agent connects back to the hub for each row here.
type Cluster struct {
	common.BaseNoSoftDelete
	Name            string     `gorm:"size:128;not null;uniqueIndex" json:"name"`
	Vendor          string     `gorm:"size:64;not null" json:"vendor"`
	Region          string     `gorm:"size:64;not null" json:"region"`
	Status          string     `gorm:"size:32;not null;default:offline" json:"status"`
	AgentVersion    string     `gorm:"size:32" json:"agentVersion,omitempty"`
	LastHeartbeatAt *time.Time `json:"lastHeartbeatAt,omitempty"`
}

func (Cluster) TableName() string { return "clusters" }

const (
	ClusterStatusOnline  = "online"
	ClusterStatusOffline = "offline"
)
