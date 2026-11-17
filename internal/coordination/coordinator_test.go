package coordination

import (
	"context"
	"sync"
	"testing"
	"time"

	coordmodels "github.com/rouroumaibing/software-distribution-platform-hub/internal/coordination/models"
)

// fakeLeases mimics the LeaseStore semantics in memory: holder==self, empty
// holder or an explicitly expired lease lets the caller take over.
type fakeLeases struct {
	mu       sync.Mutex
	rows     map[string]string
	expired  map[string]bool
	released [][2]string // {key, identity} release attempts
}

func newFakeLeases() *fakeLeases {
	return &fakeLeases{rows: map[string]string{}, expired: map[string]bool{}}
}

func (f *fakeLeases) Acquire(_ context.Context, key, identity string, _ time.Duration) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	h, ok := f.rows[key]
	if !ok || h == identity || h == "" || f.expired[key] {
		// 接管/续约即刷新租约（对应真实 DB 的 updated_at = now()），
		// 超时旗标随之解除。
		delete(f.expired, key)
		f.rows[key] = identity
		return identity, nil
	}
	return h, nil
}

func (f *fakeLeases) Release(_ context.Context, key, identity string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.released = append(f.released, [2]string{key, identity})
	if f.rows[key] == identity {
		f.rows[key] = ""
	}
	return nil
}

func (f *fakeLeases) setHolder(key, holder string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rows[key] = holder
}

func (f *fakeLeases) expire(key string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.expired[key] = true
}

// fakeWorkers records registry calls and serves a canned active-worker list.
type fakeWorkers struct {
	mu           sync.Mutex
	registers    int
	heartbeats   int
	deregistered bool
	reaped       int64
	list         []coordmodels.Worker
}

func (w *fakeWorkers) Register(_ context.Context, _, _ string) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.registers++
	return nil
}

func (w *fakeWorkers) Heartbeat(_ context.Context, _, _ string) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.heartbeats++
	return nil
}

func (w *fakeWorkers) Deregister(_ context.Context, _, _ string) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.deregistered = true
	return nil
}

func (w *fakeWorkers) ListActive(_ context.Context, _ string, _ time.Duration) ([]coordmodels.Worker, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([]coordmodels.Worker(nil), w.list...), nil
}

func (w *fakeWorkers) ReapStale(_ context.Context, _ time.Duration) (int64, error) {
	return w.reaped, nil
}

func (w *fakeWorkers) setList(podIPs ...string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.list = nil
	for i, ip := range podIPs {
		w.list = append(w.list, coordmodels.Worker{
			PodIP:     ip,
			CreatedAt: time.Unix(int64(1700000000+i), 0),
		})
	}
}

func testOpts(clusterID, podIP string) Options {
	return Options{
		ClusterID:       clusterID,
		PodIP:           podIP,
		RenewalInterval: time.Second,
		LeaseTTL:        10 * time.Second,
		HeartbeatWindow: 10 * time.Second,
		ShardCount:      4,
	}
}

func TestTickFreshInstanceBecomesMaster(t *testing.T) {
	fl, fw := newFakeLeases(), &fakeWorkers{}
	c := newCoordinator(fl, fw, testOpts("a", "p1"))

	c.tick(context.Background())

	if c.Role() != RoleMaster {
		t.Fatalf("role = %s, want master", c.Role())
	}
	if !c.instHeld.Load() {
		t.Fatal("instance lock not marked held")
	}
	if got := c.MasterHint(); got != "p1" {
		t.Fatalf("master hint = %q, want p1", got)
	}
	if fw.heartbeats != 1 {
		t.Fatalf("heartbeats = %d, want 1", fw.heartbeats)
	}
}

func TestClusterLossDemotesImmediatelyAndReleasesInstanceLock(t *testing.T) {
	fl, fw := newFakeLeases(), &fakeWorkers{}
	c := newCoordinator(fl, fw, testOpts("a", "p1"))
	c.tick(context.Background()) // become master
	if c.Role() != RoleMaster {
		t.Fatalf("precondition: role = %s, want master", c.Role())
	}

	// 接管发生：集群锁易主，本实例上一轮仍标持实例锁。
	fl.setHolder(KeyCluster, "b")
	fl.setHolder(KeyInstance, "p1")
	c.tick(context.Background())

	if c.Role() != RoleStandby {
		t.Fatalf("role = %s, want standby (cluster condition failed)", c.Role())
	}
	if c.instHeld.Load() {
		t.Fatal("instHeld still true after cluster loss")
	}
	found := false
	for _, r := range fl.released {
		if r[0] == KeyInstance && r[1] == "p1" {
			found = true
		}
	}
	if !found {
		t.Fatal("instance lock was not released on demotion")
	}
}

func TestInstanceLossBecomesWorker(t *testing.T) {
	fl, _ := newFakeLeases(), &fakeWorkers{}
	fl.setHolder(KeyCluster, "a")
	fl.setHolder(KeyInstance, "p2")
	c := newCoordinator(fl, &fakeWorkers{}, testOpts("a", "p1"))

	c.tick(context.Background())

	if c.Role() != RoleWorker {
		t.Fatalf("role = %s, want worker", c.Role())
	}
	if got := c.MasterHint(); got != "p2" {
		t.Fatalf("master hint = %q, want p2", got)
	}
}

func TestTakeoverNoSplitBrain(t *testing.T) {
	fl := newFakeLeases()
	c1 := newCoordinator(fl, &fakeWorkers{}, testOpts("a", "p1"))
	c2 := newCoordinator(fl, &fakeWorkers{}, testOpts("b", "p2"))
	ctx := context.Background()

	c1.tick(ctx) // 主集群实例成为 master
	c2.tick(ctx) // 备集群实例：集群锁被 a 持有且未过期 → standby
	if c1.Role() != RoleMaster || c2.Role() != RoleStandby {
		t.Fatalf("pre-takeover roles: c1=%s c2=%s, want master/standby", c1.Role(), c2.Role())
	}

	// 主集群全部实例停止续约：两把租约超时，备集群整体接管。
	fl.expire(KeyCluster)
	fl.expire(KeyInstance)
	c2.tick(ctx)
	if c2.Role() != RoleMaster {
		t.Fatalf("post-takeover c2 role = %s, want master", c2.Role())
	}

	// 旧主恢复续约：集群条件失败 → 立即非 master，不出现双主。
	c1.tick(ctx)
	if c1.Role() != RoleStandby {
		t.Fatalf("recovered old master role = %s, want standby", c1.Role())
	}
	if c2.Role() != RoleMaster {
		t.Fatalf("new master demoted without cause: %s", c2.Role())
	}
}

func TestRecomputeEvenSplitAndEpochStability(t *testing.T) {
	fl, fw := newFakeLeases(), &fakeWorkers{}
	fw.setList("p1", "p2")
	c := newCoordinator(fl, fw, testOpts("a", "p1"))
	ctx := context.Background()

	c.tick(ctx)
	shards1, epoch1 := c.AssignmentFor("p1")
	if got, _ := c.AssignmentFor("p2"); len(got) != 2 {
		t.Fatalf("p2 shards = %v, want 2 of 4", got)
	}
	if len(shards1) != 2 || epoch1 != 1 {
		t.Fatalf("p1 shards=%v epoch=%d, want 2 shards epoch 1", shards1, epoch1)
	}

	// worker 集合不变 → 映射不变 → epoch 不动，worker 不清队列。
	c.tick(ctx)
	if _, epoch := c.AssignmentFor("p1"); epoch != 1 {
		t.Fatalf("epoch drifted without a mapping change: %d", epoch)
	}

	// 扩容一个 worker → 全量重映射，epoch 自增。
	fw.setList("p1", "p2", "p3")
	c.tick(ctx)
	shards, epoch := c.AssignmentFor("p1")
	if epoch != 2 {
		t.Fatalf("epoch = %d, want 2 after remap", epoch)
	}
	if len(shards) != 2 || shards[0] != 0 || shards[1] != 3 {
		t.Fatalf("p1 shards = %v, want [0 3]", shards)
	}
}

func TestDrainReleasesInstanceLockAndDeregisters(t *testing.T) {
	fl, fw := newFakeLeases(), &fakeWorkers{}
	c := newCoordinator(fl, fw, testOpts("a", "p1"))
	c.tick(context.Background()) // master, holds instance lock

	if err := c.Drain(context.Background()); err != nil {
		t.Fatalf("drain: %v", err)
	}
	if got := fl.rows[KeyInstance]; got != "" {
		t.Fatalf("instance lock holder = %q, want empty after drain", got)
	}
	if !fw.deregistered {
		t.Fatal("worker registration not deleted")
	}
	if !c.stopping.Load() {
		t.Fatal("stopping flag not set")
	}
	// 二次 drain 幂等。
	if err := c.Drain(context.Background()); err != nil {
		t.Fatalf("second drain: %v", err)
	}
}

func TestRunExitsWhenStopping(t *testing.T) {
	c := newCoordinator(newFakeLeases(), &fakeWorkers{}, testOpts("a", "p1"))
	c.stopping.Store(true)
	if err := c.Run(context.Background()); err != nil {
		t.Fatalf("run with stopping pre-set: %v", err)
	}
}

func TestOneShotLockExclusiveWithinWindow(t *testing.T) {
	fl := newFakeLeases()
	c1 := newCoordinator(fl, &fakeWorkers{}, testOpts("a", "p1"))
	c2 := newCoordinator(fl, &fakeWorkers{}, testOpts("a", "p2"))
	ctx := context.Background()

	won, err := c1.OneShotLock(ctx, "backfill")
	if err != nil || !won {
		t.Fatalf("first winner: won=%v err=%v", won, err)
	}
	won, err = c2.OneShotLock(ctx, "backfill")
	if err != nil || won {
		t.Fatalf("second caller must lose: won=%v err=%v", won, err)
	}
	fl.expire(TaskKeyPrefix + "backfill")
	won, err = c2.OneShotLock(ctx, "backfill")
	if err != nil || !won {
		t.Fatalf("post-expiry takeover: won=%v err=%v", won, err)
	}
}

func TestSyncWorkloadAssignmentFollowsRole(t *testing.T) {
	fl, fw := newFakeLeases(), &fakeWorkers{}
	fw.setList("p1", "p2")
	c := newCoordinator(fl, fw, testOpts("a", "p1"))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c.AttachWorkload(ctx, &fakeSource{}, &fakeConsumer{}, QueueOptions{ConsumeRate: 100})
	defer c.queueManager().Stop()

	c.tick(ctx) // master：本地缓存直接给分片
	qm := c.queueManager()
	qm.mu.Lock()
	shards := append([]int(nil), qm.shards...)
	qm.mu.Unlock()
	want, _ := c.AssignmentFor("p1")
	if len(shards) != len(want) {
		t.Fatalf("qm shards = %v, want %v", shards, want)
	}

	// standby：分片清空。
	fl.setHolder(KeyCluster, "b")
	c.tick(ctx)
	qm.mu.Lock()
	shards = append([]int(nil), qm.shards...)
	qm.mu.Unlock()
	if len(shards) != 0 {
		t.Fatalf("standby qm shards = %v, want empty", shards)
	}
}
