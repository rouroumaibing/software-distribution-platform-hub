package coordination

import (
	"context"
	"os"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	coordmodels "github.com/rouroumaibing/software-distribution-platform-hub/internal/coordination/models"
)

// pgTestDB opens a Postgres handle when COORDINATION_TEST_DSN is set; the
// suite stays skipped by default so `go test ./...` works without a database.
func pgTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := os.Getenv("COORDINATION_TEST_DSN")
	if dsn == "" {
		t.Skip("COORDINATION_TEST_DSN not set — Postgres-backed lease semantics test skipped")
	}
	gdb, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatalf("open pg: %v", err)
	}
	if err := gdb.AutoMigrate(&coordmodels.Lease{}, &coordmodels.Worker{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	t.Cleanup(func() {
		gdb.Exec(`DELETE FROM coord_leases`)
		gdb.Exec(`DELETE FROM coord_workers`)
		if sqlDB, err := gdb.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})
	return gdb
}

func TestLeaseStoreAcquireBranches(t *testing.T) {
	gdb := pgTestDB(t)
	store := NewLeaseStore(gdb)
	ctx := context.Background()

	// 行缺则插入：首抢即持有。
	if h, err := store.Acquire(ctx, "t1", "p1", time.Minute); err != nil || h != "p1" {
		t.Fatalf("fresh acquire: holder=%q err=%v", h, err)
	}
	// 持有者是自己：续约成立。
	if h, err := store.Acquire(ctx, "t1", "p1", time.Minute); err != nil || h != "p1" {
		t.Fatalf("self renew: holder=%q err=%v", h, err)
	}
	// 持有者是他人且未超时：拿不到，返回真实持有者。
	if h, err := store.Acquire(ctx, "t1", "p2", time.Minute); err != nil || h != "p1" {
		t.Fatalf("contended acquire: holder=%q err=%v", h, err)
	}
	// 显式让位（置空）后：空持有者立即易主。
	if err := store.Release(ctx, "t1", "p1"); err != nil {
		t.Fatalf("release: %v", err)
	}
	if h, err := store.Acquire(ctx, "t1", "p2", time.Minute); err != nil || h != "p2" {
		t.Fatalf("acquire after yield: holder=%q err=%v", h, err)
	}
	// 租约超时（DB 时钟判定）：他人可接管。
	gdb.Exec(`UPDATE coord_leases SET updated_at = now() - interval '2 minutes' WHERE "key" = 't1'`)
	if h, err := store.Acquire(ctx, "t1", "p1", time.Minute); err != nil || h != "p1" {
		t.Fatalf("expired takeover: holder=%q err=%v", h, err)
	}
}

func TestLeaseStoreConcurrentAcquireSingleWinner(t *testing.T) {
	gdb := pgTestDB(t)
	store := NewLeaseStore(gdb)
	ctx := context.Background()

	const contenders = 8
	const rounds = 20
	var mu sync.Mutex
	errs := 0
	var wg sync.WaitGroup
	for i := 0; i < contenders; i++ {
		identity := "p" + string(rune('A'+i))
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < rounds; j++ {
				if _, err := store.Acquire(ctx, "contended", identity, time.Minute); err != nil {
					mu.Lock()
					errs++
					mu.Unlock()
				}
				// Yield to interleave contenders deterministically without a wall-clock sleep.
				runtime.Gosched()
			}
		}()
	}
	wg.Wait()
	if errs != 0 {
		t.Fatalf("%d acquire errors under contention", errs)
	}
	var holder string
	if err := gdb.Raw(`SELECT holder FROM coord_leases WHERE "key" = 'contended'`).Scan(&holder).Error; err != nil {
		t.Fatalf("read holder: %v", err)
	}
	if holder == "" {
		t.Fatal("no winner after contention")
	}
}

func TestWorkerRegistryUpsertKeepsCreatedAt(t *testing.T) {
	gdb := pgTestDB(t)
	reg := NewWorkerRegistry(gdb)
	ctx := context.Background()

	if err := reg.Register(ctx, "a", "10.0.0.1"); err != nil {
		t.Fatalf("register: %v", err)
	}
	var first coordmodels.Worker
	if err := gdb.Raw(`SELECT * FROM coord_workers WHERE cluster_id = 'a' AND pod_ip = '10.0.0.1'`).Scan(&first).Error; err != nil {
		t.Fatalf("read worker: %v", err)
	}
	if first.ID == uuid.Nil {
		t.Fatal("worker id not generated")
	}

	// 心跳推进 last_heartbeat，created_at 保持（确定性排序键）。
	// Wait for the DB clock to advance past created_at so the heartbeat
	// timestamp is strictly later — explicit condition, no fixed sleep.
	require.Eventually(t, func() bool {
		var ahead bool
		if err := gdb.Raw(`SELECT now() > ?`, first.CreatedAt).Scan(&ahead).Error; err != nil {
			return false
		}
		return ahead
	}, 2*time.Second, time.Millisecond, "DB clock did not advance past created_at")
	if err := reg.Heartbeat(ctx, "a", "10.0.0.1"); err != nil {
		t.Fatalf("heartbeat: %v", err)
	}
	var second coordmodels.Worker
	if err := gdb.Raw(`SELECT * FROM coord_workers WHERE cluster_id = 'a' AND pod_ip = '10.0.0.1'`).Scan(&second).Error; err != nil {
		t.Fatalf("reread worker: %v", err)
	}
	if !second.CreatedAt.Equal(first.CreatedAt) {
		t.Fatalf("created_at drifted: %v -> %v", first.CreatedAt, second.CreatedAt)
	}
	if !second.LastHeartbeat.After(first.LastHeartbeat) {
		t.Fatal("last_heartbeat not advanced by heartbeat")
	}

	// ListActive 心跳窗过滤 + created_at 升序。
	if err := reg.Register(ctx, "a", "10.0.0.2"); err != nil {
		t.Fatalf("register second: %v", err)
	}
	gdb.Exec(`UPDATE coord_workers SET last_heartbeat = now() - interval '1 hour' WHERE pod_ip = '10.0.0.2'`)
	active, err := reg.ListActive(ctx, "a", 10*time.Second)
	if err != nil {
		t.Fatalf("list active: %v", err)
	}
	if len(active) != 1 || active[0].PodIP != "10.0.0.1" {
		t.Fatalf("active = %+v, want only 10.0.0.1", active)
	}

	if n, err := reg.ReapStale(ctx, time.Minute); err != nil || n != 1 {
		t.Fatalf("reap stale: n=%d err=%v, want 1", n, err)
	}
	if err := reg.Deregister(ctx, "a", "10.0.0.1"); err != nil {
		t.Fatalf("deregister: %v", err)
	}
	var count int64
	gdb.Raw(`SELECT count(*) FROM coord_workers`).Scan(&count)
	if count != 0 {
		t.Fatalf("rows after deregister = %d, want 0", count)
	}
}
