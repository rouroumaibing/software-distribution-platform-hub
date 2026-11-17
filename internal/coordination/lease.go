package coordination

import (
	"context"
	"time"

	"gorm.io/gorm"
)

// LeaseBackend is the lease primitive the coordinator builds on: one atomic
// "acquire or renew" path plus an explicit release. The abstraction exists so
// the role loop is unit-testable without Postgres.
type LeaseBackend interface {
	// Acquire runs the row-locked transaction (§4.1): insert the row when
	// missing, then under FOR UPDATE take over when holder==identity, holder
	// is empty or the lease expired, and return the resulting holder.
	// Callers treat "returned holder == identity" as ownership.
	Acquire(ctx context.Context, key, identity string, ttl time.Duration) (string, error)
	// Release empties the holder (only when it currently is identity) so the
	// next acquirer skips the TTL wait — the graceful-yield path.
	Release(ctx context.Context, key, identity string) error
}

// LeaseStore is the gorm-backed LeaseBackend. Every time comparison runs
// inside SQL (now() vs updated_at), so instance and cross-cluster clock skew
// never influence expiry decisions (§2.2).
type LeaseStore struct {
	db *gorm.DB
}

// NewLeaseStore builds a LeaseStore on the shared gorm handle.
func NewLeaseStore(db *gorm.DB) *LeaseStore { return &LeaseStore{db: db} }

// Acquire implements LeaseBackend.Acquire. The four steps match §4.1 exactly:
// insert-when-missing → SELECT ... FOR UPDATE → conditional overwrite →
// return the post-transaction holder.
func (s *LeaseStore) Acquire(ctx context.Context, key, identity string, ttl time.Duration) (string, error) {
	var holder string
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// 行缺则插入：并发首抢由 ON CONFLICT 收敛为单行，随后 FOR UPDATE 串行判定。
		if err := tx.Exec(
			`INSERT INTO coord_leases ("key", holder, updated_at)
			 VALUES (?, ?, now()) ON CONFLICT ("key") DO NOTHING`,
			key, identity,
		).Error; err != nil {
			return err
		}
		var row struct {
			Holder  string
			Expired bool
		}
		if err := tx.Raw(
			`SELECT holder, (updated_at < now() - make_interval(secs => ?)) AS expired
			 FROM coord_leases WHERE "key" = ? FOR UPDATE`,
			ttl.Seconds(), key,
		).Scan(&row).Error; err != nil {
			return err
		}
		holder = row.Holder
		if holder == identity || holder == "" || row.Expired {
			if err := tx.Exec(
				`UPDATE coord_leases SET holder = ?, updated_at = now() WHERE "key" = ?`,
				identity, key,
			).Error; err != nil {
				return err
			}
			holder = identity
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	return holder, nil
}

// Release implements LeaseBackend.Release. The conditional update makes the
// yield idempotent and harmless when ownership has already moved on.
func (s *LeaseStore) Release(ctx context.Context, key, identity string) error {
	return s.db.WithContext(ctx).Exec(
		`UPDATE coord_leases SET holder = '', updated_at = now() WHERE "key" = ? AND holder = ?`,
		key, identity,
	).Error
}
