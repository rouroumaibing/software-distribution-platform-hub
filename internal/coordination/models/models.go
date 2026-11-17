// Package models holds the coordination framework's persisted rows
// (coordination-design.md §3.1). All coordination state lives in these two
// tables — instances keep no local durable state and can be recreated at any
// time.
package models

import (
	"time"

	"github.com/google/uuid"
)

// Lease is one row of coord_leases: the shared-Postgres lease backing the
// cluster lock ("cluster"), the instance lock ("instance") and one-shot task
// locks ("task:<name>"). Holder is the owner identity — the cluster ID for
// the collective cluster lease, the PodIP for the individual instance lease;
// an empty holder means the owner yielded explicitly. Expiry decisions run
// in SQL against the DB clock (coordination package §4.1).
type Lease struct {
	Key       string    `gorm:"primaryKey;size:64" json:"key"`
	Holder    string    `gorm:"size:64;not null;default:''" json:"holder"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// TableName pins the GORM table name.
func (Lease) TableName() string { return "coord_leases" }

// Worker is one row of coord_workers: instance registration and heartbeat in
// a single row. CreatedAt is fixed on first registration and serves as the
// stable ordering key for the shard mapping (§2.4: workers sorted by
// created_at); LastHeartbeat is refreshed by the renewal loop, and rows with
// a stale heartbeat are excluded from the master's recompute.
type Worker struct {
	ID            uuid.UUID `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"id"`
	ClusterID     string    `gorm:"size:64;uniqueIndex:uk_coord_worker_cluster_pod" json:"clusterId"`
	PodIP         string    `gorm:"size:64;uniqueIndex:uk_coord_worker_cluster_pod" json:"podIp"`
	CreatedAt     time.Time `json:"createdAt"`
	LastHeartbeat time.Time `json:"lastHeartbeat"`
}

// TableName pins the GORM table name.
func (Worker) TableName() string { return "coord_workers" }
