package coordination

import (
	"context"
	"time"

	"gorm.io/gorm"

	coordmodels "github.com/rouroumaibing/software-distribution-platform-hub/internal/coordination/models"
)

// WorkerBackend is the registration/heartbeat surface the coordinator uses.
// The abstraction exists so the role loop is unit-testable without Postgres.
type WorkerBackend interface {
	// Register upserts the instance row; created_at is fixed on first insert.
	Register(ctx context.Context, clusterID, podIP string) error
	// Heartbeat refreshes last_heartbeat; the upsert doubles as registration,
	// so a heartbeat after a crash-induced row loss restores presence.
	Heartbeat(ctx context.Context, clusterID, podIP string) error
	// Deregister deletes the instance row (drain step 3, §4.6) — the master
	// redistributes the shards on its next recompute without waiting for the
	// heartbeat window.
	Deregister(ctx context.Context, clusterID, podIP string) error
	// ListActive returns the workers of clusterID whose heartbeat is fresh
	// (within window), ordered by created_at ascending — the deterministic
	// order the even shard split is computed on (§2.4).
	ListActive(ctx context.Context, clusterID string, window time.Duration) ([]coordmodels.Worker, error)
	// ReapStale deletes rows whose last heartbeat is older than horizon
	// (instances that crashed without a drain). Returns the deleted count;
	// reaping is hygiene, freshness filtering is the correctness gate.
	ReapStale(ctx context.Context, horizon time.Duration) (int64, error)
}

// WorkerRegistry is the gorm-backed WorkerBackend. Presence math runs in SQL
// against the DB clock, consistent with the lease expiry semantics.
type WorkerRegistry struct {
	db *gorm.DB
}

// NewWorkerRegistry builds a WorkerRegistry on the shared gorm handle.
func NewWorkerRegistry(db *gorm.DB) *WorkerRegistry { return &WorkerRegistry{db: db} }

// Register implements WorkerBackend.Register.
func (r *WorkerRegistry) Register(ctx context.Context, clusterID, podIP string) error {
	return r.upsert(ctx, clusterID, podIP)
}

// Heartbeat implements WorkerBackend.Heartbeat.
func (r *WorkerRegistry) Heartbeat(ctx context.Context, clusterID, podIP string) error {
	return r.upsert(ctx, clusterID, podIP)
}

// upsert is the shared insert path: first insert fixes created_at, conflicts
// only refresh last_heartbeat so the ordering key stays stable.
func (r *WorkerRegistry) upsert(ctx context.Context, clusterID, podIP string) error {
	return r.db.WithContext(ctx).Exec(
		`INSERT INTO coord_workers (id, cluster_id, pod_ip, created_at, last_heartbeat)
		 VALUES (gen_random_uuid(), ?, ?, now(), now())
		 ON CONFLICT (cluster_id, pod_ip) DO UPDATE SET last_heartbeat = EXCLUDED.last_heartbeat`,
		clusterID, podIP,
	).Error
}

// Deregister implements WorkerBackend.Deregister.
func (r *WorkerRegistry) Deregister(ctx context.Context, clusterID, podIP string) error {
	return r.db.WithContext(ctx).Exec(
		`DELETE FROM coord_workers WHERE cluster_id = ? AND pod_ip = ?`,
		clusterID, podIP,
	).Error
}

// ListActive implements WorkerBackend.ListActive.
func (r *WorkerRegistry) ListActive(ctx context.Context, clusterID string, window time.Duration) ([]coordmodels.Worker, error) {
	var workers []coordmodels.Worker
	err := r.db.WithContext(ctx).Raw(
		`SELECT * FROM coord_workers
		 WHERE cluster_id = ? AND last_heartbeat >= now() - make_interval(secs => ?)
		 ORDER BY created_at ASC`,
		clusterID, window.Seconds(),
	).Scan(&workers).Error
	return workers, err
}

// ReapStale implements WorkerBackend.ReapStale.
func (r *WorkerRegistry) ReapStale(ctx context.Context, horizon time.Duration) (int64, error) {
	res := r.db.WithContext(ctx).Exec(
		`DELETE FROM coord_workers WHERE last_heartbeat < now() - make_interval(secs => ?)`,
		horizon.Seconds(),
	)
	return res.RowsAffected, res.Error
}
