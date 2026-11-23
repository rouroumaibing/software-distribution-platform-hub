package coordination

import (
	"context"
	"os"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	applog "github.com/rouroumaibing/software-distribution-platform-hub/internal/common/logger"
	coordmodels "github.com/rouroumaibing/software-distribution-platform-hub/internal/coordination/models"

	"gorm.io/gorm"
)

// Role is the per-instance coordination role, re-derived every renewal tick
// (§2.3). master = cluster lock held by own cluster AND instance lock held by
// own PodIP; worker = cluster condition only; standby = cluster condition
// failed. The cluster condition is the prerequisite — an instance losing
// cluster-lock eligibility is non-master immediately, which is what keeps the
// takeover split-brain-free.
type Role int32

const (
	RoleStandby Role = iota
	RoleWorker
	RoleMaster
)

// String renders the role for logs and diagnostics.
func (r Role) String() string {
	switch r {
	case RoleMaster:
		return "master"
	case RoleWorker:
		return "worker"
	default:
		return "standby"
	}
}

// Lease keys (§2.3). The cluster lock is a collective lease: any instance of
// the leader cluster renews the same row with the cluster ID, so it expires
// only when every instance of that cluster stops renewing. The instance lock
// is individual: its unique holder (PodIP) is the master. One-shot task locks
// use the "task:<name>" prefix.
const (
	KeyCluster    = "cluster"
	KeyInstance   = "instance"
	TaskKeyPrefix = "task:"
)

// Defaults mirror coordination-design.md §2.8.
const (
	DefaultRenewalInterval = 2 * time.Second
	DefaultLeaseTTL        = 10 * time.Second
	DefaultHeartbeatWindow = 10 * time.Second
	DefaultStaleHorizon    = time.Hour
	DefaultShardCount      = 16
)

// OneShotTTL is the non-renewed lease for one-shot task locks (§2.2): the
// winner runs once; re-running inside the window waits for expiry, and
// "run once" guarantees live in the task's own state rows.
const OneShotTTL = 10 * time.Minute

// Options configures one coordinator.
type Options struct {
	// ClusterID is the cluster-lock identity (SDP_CLUSTER_ID): the primary
	// and standby deployments take different values.
	ClusterID string
	// PodIP is the instance-lock identity (Downward API status.podIP).
	PodIP string
	// HTTPPort is the hub listen port pods use to reach each other for the
	// internal report endpoint (derived from the hub Addr at wiring time).
	HTTPPort int

	RenewalInterval time.Duration
	LeaseTTL        time.Duration
	HeartbeatWindow time.Duration
	StaleHorizon    time.Duration
	ShardCount      int
}

// fill applies §2.8 defaults for unset fields.
func (o *Options) fill() {
	if o.RenewalInterval <= 0 {
		o.RenewalInterval = DefaultRenewalInterval
	}
	if o.LeaseTTL <= 0 {
		o.LeaseTTL = DefaultLeaseTTL
	}
	if o.HeartbeatWindow <= 0 {
		o.HeartbeatWindow = DefaultHeartbeatWindow
	}
	if o.StaleHorizon <= 0 {
		o.StaleHorizon = DefaultStaleHorizon
	}
	if o.ShardCount <= 0 {
		o.ShardCount = DefaultShardCount
	}
}

// masterSnapshot is the master's view, recomputed from shared tables every
// renewal tick (§2.4). Guarded by the coordinator's cacheMu: the renewal
// goroutine writes while the report handler reads concurrently.
type masterSnapshot struct {
	workers []coordmodels.Worker
	mapping map[int]string // shard number -> PodIP
	epoch   uint64         // bumped only when the mapping changes
}

// Coordinator owns the two-level election for one hub instance. It is safe
// for concurrent use: the role is read atomically by the report handler and
// business callers, the master cache is RWMutex-guarded.
type Coordinator struct {
	leases  LeaseBackend
	workers WorkerBackend
	opts    Options

	role       atomic.Int32
	stopping   atomic.Bool
	instHeld   atomic.Bool  // instance lock held as of the last tick
	masterHint atomic.Value // string — last observed instance-lock holder

	cacheMu sync.RWMutex
	cache   masterSnapshot

	mu       sync.Mutex
	queues   *ShardQueueManager
	reporter *reportClient
}

// NewCoordinator builds a coordinator on the shared gorm handle.
func NewCoordinator(gdb *gorm.DB, opts Options) *Coordinator {
	return newCoordinator(NewLeaseStore(gdb), NewWorkerRegistry(gdb), opts)
}

func newCoordinator(leases LeaseBackend, workers WorkerBackend, opts Options) *Coordinator {
	opts.fill()
	c := &Coordinator{leases: leases, workers: workers, opts: opts}
	c.masterHint.Store("")
	return c
}

// Run drives the renewal loop until ctx is cancelled or Drain is called
// (§4.2). Each tick: acquire the cluster lock → gate the instance lock on
// cluster eligibility → heartbeat → re-derive the role → (as master)
// recompute the shard mapping.
func (c *Coordinator) Run(ctx context.Context) error {
	if err := c.workers.Register(ctx, c.opts.ClusterID, c.opts.PodIP); err != nil {
		// Registration doubles as the first heartbeat; a failed start register
		// self-heals on the next tick's heartbeat upsert.
		applog.Infof("coordination: initial register failed (will self-heal): %v", err)
	}
	ticker := time.NewTicker(c.opts.RenewalInterval)
	defer ticker.Stop()
	for {
		if c.stopping.Load() {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			c.tick(ctx)
		}
	}
}

// tick is one renewal round (§2.3). DB errors land on standby — when the
// shared state cannot be observed, acting would be speculation; the TTL
// machinery handles the rest.
func (c *Coordinator) tick(ctx context.Context) {
	// 1. 集群锁（集体租约，身份=集群 ID）。
	clusterHolder, err := c.leases.Acquire(ctx, KeyCluster, c.opts.ClusterID, c.opts.LeaseTTL)
	if err != nil {
		applog.Infof("coordination: cluster lock acquire failed: %v", err)
		c.setRole(RoleStandby)
		c.syncWorkloadAssignment(ctx)
		return
	}
	if clusterHolder != c.opts.ClusterID {
		// 失集群锁资格 → 立即非 master（§2.3）。若上一轮仍持实例锁（接管竞态
		// 下恢复的一方可能短暂重持），显式置空加速新主接管，随后静默。
		if c.instHeld.CompareAndSwap(true, false) {
			if rerr := c.leases.Release(ctx, KeyInstance, c.opts.PodIP); rerr != nil {
				applog.Infof("coordination: instance lock release on demotion failed: %v", rerr)
			}
		}
		c.setRole(RoleStandby)
		c.syncWorkloadAssignment(ctx)
		return
	}

	// 2. 实例锁（个体租约，身份=Pod IP；集群 leader 资格是抢锁前提——
	// standby 实例永远不可能持实例锁）。
	instHolder, err := c.leases.Acquire(ctx, KeyInstance, c.opts.PodIP, c.opts.LeaseTTL)
	if err != nil {
		applog.Infof("coordination: instance lock acquire failed: %v", err)
		c.setRole(RoleWorker)
		c.syncWorkloadAssignment(ctx)
		return
	}
	c.masterHint.Store(instHolder)
	held := instHolder == c.opts.PodIP
	c.instHeld.Store(held)

	// 3. 心跳：注册与心跳合一的 upsert（§2.4，不另设心跳通道）。
	if err := c.workers.Heartbeat(ctx, c.opts.ClusterID, c.opts.PodIP); err != nil {
		applog.Infof("coordination: heartbeat failed: %v", err)
	}

	// 4. 角色重估 + 职责同步（master 重算映射；任一角色都把当前分片视图
	// 推给队列管理器，standby 即清空——不领分片不消费）。
	role := RoleWorker
	if held {
		role = RoleMaster
	}
	prev := c.setRole(role)
	if role == RoleMaster {
		c.recompute(ctx)
	}
	c.syncWorkloadAssignment(ctx)
	if prev != role {
		applog.Infof("coordination: became %s (cluster %s holds the cluster lock, instance lock held=%v)",
			role, c.opts.ClusterID, held)
	}
}

// setRole swaps the role atomically and returns the previous value.
func (c *Coordinator) setRole(r Role) Role {
	return Role(c.role.Swap(int32(r)))
}

// Role returns the current role; safe from any goroutine (§4.2). Business
// callers use it to decide master-only duties.
func (c *Coordinator) Role() Role { return Role(c.role.Load()) }

// MasterHint returns the last observed instance-lock holder — the presumed
// master PodIP that producers route their reports to (§4.4).
func (c *Coordinator) MasterHint() string {
	if v, ok := c.masterHint.Load().(string); ok {
		return v
	}
	return ""
}

// recompute rebuilds the master view from shared tables (§2.4): active
// workers of the leader cluster in created_at order, shards split evenly
// (shard i -> workers[i mod N]); the epoch bumps only when the mapping
// changes so workers never reset queues without a reason. Rows beyond the
// stale horizon are reaped opportunistically.
func (c *Coordinator) recompute(ctx context.Context) {
	ws, err := c.workers.ListActive(ctx, c.opts.ClusterID, c.opts.HeartbeatWindow)
	if err != nil {
		applog.Infof("coordination: list active workers failed: %v", err)
		return
	}
	mapping := make(map[int]string, c.opts.ShardCount)
	if len(ws) > 0 {
		for i := 0; i < c.opts.ShardCount; i++ {
			mapping[i] = ws[i%len(ws)].PodIP
		}
	}
	if n, err := c.workers.ReapStale(ctx, c.opts.StaleHorizon); err == nil && n > 0 {
		applog.Infof("coordination: reaped %d stale worker rows", n)
	}

	c.cacheMu.Lock()
	if !mappingEqual(c.cache.mapping, mapping) {
		c.cache.mapping = mapping
		c.cache.epoch++
	}
	c.cache.workers = ws
	c.cacheMu.Unlock()
}

// AssignmentFor returns the shards currently mapped to podIP plus the mapping
// epoch; the master serves report requests straight from this view (§4.4).
func (c *Coordinator) AssignmentFor(podIP string) ([]int, uint64) {
	c.cacheMu.RLock()
	defer c.cacheMu.RUnlock()
	shards := make([]int, 0, c.opts.ShardCount/len(c.cache.mapping)+1)
	for s, ip := range c.cache.mapping {
		if ip == podIP {
			shards = append(shards, s)
		}
	}
	sort.Ints(shards)
	return shards, c.cache.epoch
}

// OneShotLock applies the standard Acquire semantics with a 10-minute TTL and
// no renewal loop (§2.2): true means this instance won the right to run the
// task in the current window.
func (c *Coordinator) OneShotLock(ctx context.Context, name string) (bool, error) {
	holder, err := c.leases.Acquire(ctx, TaskKeyPrefix+name, c.opts.PodIP, OneShotTTL)
	if err != nil {
		return false, err
	}
	return holder == c.opts.PodIP, nil
}

// Drain performs the graceful-stop routine (§4.6) in the fixed hierarchy
// order: exit cluster-lock renewal (the loop stops via the stopping flag — a
// collective lease is never emptied by a single member), explicitly empty the
// instance lock when held, then delete the worker registration so the master
// redistributes the shards on its next recompute. Idempotent; shared by the
// authenticated HTTP endpoint and the SIGTERM path.
func (c *Coordinator) Drain(ctx context.Context) error {
	if !c.stopping.CompareAndSwap(false, true) {
		return nil
	}
	applog.Infof("coordination: draining (cluster %s, pod %s)", c.opts.ClusterID, c.opts.PodIP)
	var firstErr error
	if c.instHeld.Load() {
		if err := c.leases.Release(ctx, KeyInstance, c.opts.PodIP); err != nil {
			firstErr = err
			applog.Infof("coordination: instance lock release during drain failed: %v", err)
		}
		c.instHeld.Store(false)
	}
	if err := c.workers.Deregister(ctx, c.opts.ClusterID, c.opts.PodIP); err != nil && firstErr == nil {
		firstErr = err
		applog.Infof("coordination: deregister during drain failed: %v", err)
	}
	if qm := c.queueManager(); qm != nil {
		qm.Stop()
	}
	return firstErr
}

// AttachWorkload wires the sharded background work (§2.6) and starts its
// queues. Business binding is deliberately not preset: dispatch_jobs re-drain
// and agent-op sweeps attach here once their shard dimension is decided.
//
// Deprecated: this method is not called from the hub main path; only tests use
// the shard queue framework today.
func (c *Coordinator) AttachWorkload(ctx context.Context, src ShardSource, consumer TaskConsumer, qopts QueueOptions) {
	qm := NewShardQueueManager(src, consumer, qopts)
	qm.Start(ctx)
	c.mu.Lock()
	c.queues = qm
	c.reporter = newReportClient(c.opts.HTTPPort)
	c.mu.Unlock()
}

func (c *Coordinator) queueManager() *ShardQueueManager {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.queues
}

// syncWorkloadAssignment pushes the current shard view into the queue
// manager (§2.5): as master the assignment comes from the local cache, as
// worker it comes from a report to the current master (409 → follow the
// returned holder on the next tick). Without an attached workload this is a
// no-op.
func (c *Coordinator) syncWorkloadAssignment(ctx context.Context) {
	c.mu.Lock()
	qm, reporter := c.queues, c.reporter
	c.mu.Unlock()
	if qm == nil {
		return
	}
	switch c.Role() {
	case RoleMaster:
		shards, epoch := c.AssignmentFor(c.opts.PodIP)
		qm.SetAssignment(shards, epoch)
	case RoleWorker:
		if reporter == nil || c.MasterHint() == "" || c.MasterHint() == c.opts.PodIP {
			return
		}
		shards, epoch, err := reporter.Report(ctx, c.MasterHint(), ReportRequest{
			PodIP:     c.opts.PodIP,
			ClusterID: c.opts.ClusterID,
		})
		if err != nil {
			// 保持旧分片继续消费；业务占坑语义兜底双消费（§2.6 挂接前提）。
			applog.Infof("coordination: report to master %s failed: %v", c.MasterHint(), err)
			return
		}
		qm.SetAssignment(shards, epoch)
	default:
		qm.SetAssignment(nil, c.currentEpoch())
	}
}

func (c *Coordinator) currentEpoch() uint64 {
	c.cacheMu.RLock()
	defer c.cacheMu.RUnlock()
	return c.cache.epoch
}

func mappingEqual(a, b map[int]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}

// ResolvePodIP derives the instance-lock identity: SDP_POD_IP (Downward API
// status.podIP) wins; the hostname covers single-process local runs where no
// Downward API is mounted.
func ResolvePodIP() string {
	if ip := os.Getenv("SDP_POD_IP"); ip != "" {
		return ip
	}
	if ip := os.Getenv("POD_IP"); ip != "" {
		return ip
	}
	host, err := os.Hostname()
	if err != nil {
		return "unknown"
	}
	return host
}
