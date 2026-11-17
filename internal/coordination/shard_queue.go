package coordination

import (
	"context"
	"sort"
	"sync"
	"time"

	applog "github.com/rouroumaibing/software-distribution-platform-hub/internal/common/logger"
)

// TaskRef identifies one unit of due work in the business store.
type TaskRef = string

// ShardTask is one due work item with the shard it belongs to; the queue
// manager routes it into that shard's bounded queue.
type ShardTask struct {
	Shard int
	Ref   TaskRef
}

// ShardSource is the business-provided refill feed (§4.5): given the shards
// this instance owns, list due work bounded by limit. The DB stays the source
// of truth — the in-memory queues are a transient projection, so a dropped or
// never-enqueued item is simply re-listed by a later refill (重灌).
type ShardSource interface {
	ListDue(ctx context.Context, shards []int, limit int) ([]ShardTask, error)
}

// TaskConsumer processes one item. Returning an error means "failed": the
// framework drops the item WITHOUT re-enqueueing it (poison-pill guard,
// §2.5); retry semantics live in the business store (attempts / next_retry_at)
// and the next refill naturally re-lists the row when it is due again.
type TaskConsumer interface {
	Consume(ctx context.Context, ref TaskRef) error
}

// Queue defaults mirror coordination-design.md §2.8.
const (
	DefaultQueueCapacity  = 64
	DefaultRefillInterval = 5 * time.Second
	DefaultConsumeRate    = 10
)

// QueueOptions tunes the sharded queues.
type QueueOptions struct {
	// Capacity bounds each per-shard queue; refill tops up only the free
	// space and due rows stay in the DB when the queue is full.
	Capacity int
	// RefillInterval is how often ListDue runs (重灌 cadence).
	RefillInterval time.Duration
	// ConsumeRate caps consumption in items/second across all shards; the
	// consumer pops round-robin so every shard drains at the same pace
	// (均匀消费).
	ConsumeRate int
	// ConsumeTimeout bounds a single Consume call; zero disables the bound.
	ConsumeTimeout time.Duration
}

// fill applies §2.8 defaults for unset fields.
func (o *QueueOptions) fill() {
	if o.Capacity <= 0 {
		o.Capacity = DefaultQueueCapacity
	}
	if o.RefillInterval <= 0 {
		o.RefillInterval = DefaultRefillInterval
	}
	if o.ConsumeRate <= 0 {
		o.ConsumeRate = DefaultConsumeRate
	}
}

// ShardQueueManager owns one bounded queue per assigned shard plus the refill
// and consume loops. Assignments arrive from the coordinator each renewal
// tick; any epoch change rebuilds the queues because the projection is
// rebuilt from the DB on the next refill.
type ShardQueueManager struct {
	src      ShardSource
	consumer TaskConsumer
	opts     QueueOptions

	mu     sync.Mutex
	queues map[int]chan TaskRef
	shards []int
	epoch  uint64
	rot    int // round-robin cursor for even consumption
	cancel context.CancelFunc
}

// NewShardQueueManager builds the manager; loops start via Start.
func NewShardQueueManager(src ShardSource, consumer TaskConsumer, opts QueueOptions) *ShardQueueManager {
	opts.fill()
	return &ShardQueueManager{src: src, consumer: consumer, opts: opts}
}

// Start launches the refill and consume loops; idempotent.
func (m *ShardQueueManager) Start(ctx context.Context) {
	m.mu.Lock()
	if m.cancel != nil {
		m.mu.Unlock()
		return
	}
	loopCtx, cancel := context.WithCancel(ctx)
	m.cancel = cancel
	m.mu.Unlock()
	go m.refillLoop(loopCtx)
	go m.consumeLoop(loopCtx)
}

// Stop cancels both loops; safe to call multiple times.
func (m *ShardQueueManager) Stop() {
	m.mu.Lock()
	cancel := m.cancel
	m.cancel = nil
	m.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

// SetAssignment swaps the shard set and mapping epoch (§2.5): any epoch or
// shard-set change clears all queues — the projection is rebuilt from the DB
// on the next refill, so clearing never loses work.
func (m *ShardQueueManager) SetAssignment(shards []int, epoch uint64) {
	sorted := append([]int(nil), shards...)
	sort.Ints(sorted)
	m.mu.Lock()
	defer m.mu.Unlock()
	changed := m.queues == nil || epoch != m.epoch || !intSliceEqual(m.shards, sorted)
	m.shards = sorted
	m.epoch = epoch
	if changed {
		m.queues = make(map[int]chan TaskRef, len(sorted))
		for _, s := range sorted {
			m.queues[s] = make(chan TaskRef, m.opts.Capacity)
		}
	}
}

// refillLoop re-lists due work on the refill cadence until ctx is cancelled.
func (m *ShardQueueManager) refillLoop(ctx context.Context) {
	ticker := time.NewTicker(m.opts.RefillInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.refillOnce(ctx)
		}
	}
}

// refillOnce tops the queues up to capacity. A full queue means the refill
// requests less (or nothing); the skipped due rows remain in the DB and are
// picked up on a later pass.
func (m *ShardQueueManager) refillOnce(ctx context.Context) {
	m.mu.Lock()
	shards := append([]int(nil), m.shards...)
	free := 0
	for _, s := range shards {
		if q, ok := m.queues[s]; ok {
			free += m.opts.Capacity - len(q)
		}
	}
	m.mu.Unlock()
	if len(shards) == 0 || free <= 0 {
		return
	}
	tasks, err := m.src.ListDue(ctx, shards, free)
	if err != nil {
		applog.Infof("coordination: shard refill list failed: %v", err)
		return
	}
	m.mu.Lock()
	for _, t := range tasks {
		if q, ok := m.queues[t.Shard]; ok {
			select {
			case q <- t.Ref:
			default:
				// 队满丢弃：DB 行未动，下一轮重灌再拉（§2.5）。
			}
		}
	}
	m.mu.Unlock()
}

// consumeLoop pops one item per tick, rotating across shards — one global
// rate cap with even per-shard pacing.
func (m *ShardQueueManager) consumeLoop(ctx context.Context) {
	interval := time.Duration(float64(time.Second) / float64(m.opts.ConsumeRate))
	if interval <= 0 {
		interval = time.Millisecond
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if ref, ok := m.popNext(); ok {
				m.consume(ctx, ref)
			}
		}
	}
}

// popNext scans the shard set round-robin and returns the first ready item,
// advancing the cursor so successive pops walk all shards evenly.
func (m *ShardQueueManager) popNext() (TaskRef, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	n := len(m.shards)
	if n == 0 {
		return "", false
	}
	for i := 0; i < n; i++ {
		m.rot = (m.rot + 1) % n
		q, ok := m.queues[m.shards[m.rot]]
		if !ok {
			continue
		}
		select {
		case ref := <-q:
			return ref, true
		default:
		}
	}
	return "", false
}

// consume runs the consumer outside the manager lock. Failures are dropped
// from memory only — never re-enqueued (§2.5).
func (m *ShardQueueManager) consume(ctx context.Context, ref TaskRef) {
	cctx, cancel := ctx, context.CancelFunc(func() {})
	if m.opts.ConsumeTimeout > 0 {
		cctx, cancel = context.WithTimeout(ctx, m.opts.ConsumeTimeout)
	}
	defer cancel()
	if err := m.consumer.Consume(cctx, ref); err != nil {
		applog.Infof("coordination: shard consume failed for %s (not re-enqueued, waits for refill): %v", ref, err)
	}
}

func intSliceEqual(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
