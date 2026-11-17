package coordination

import (
	"context"
	"sync"
	"testing"
)

// fakeSource serves a canned due list and records the refill call limits.
type fakeSource struct {
	mu     sync.Mutex
	tasks  []ShardTask
	limits []int
}

func (s *fakeSource) ListDue(_ context.Context, shards []int, limit int) ([]ShardTask, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.limits = append(s.limits, limit)
	return append([]ShardTask(nil), s.tasks...), nil
}

func (s *fakeSource) setTasks(tasks ...ShardTask) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tasks = tasks
}

func (s *fakeSource) callCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.limits)
}

// fakeConsumer records consumption and can fail configured refs.
type fakeConsumer struct {
	mu       sync.Mutex
	consumed []string
	failRefs map[string]bool
}

func (c *fakeConsumer) Consume(_ context.Context, ref TaskRef) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.consumed = append(c.consumed, ref)
	if c.failRefs[ref] {
		return errTestConsume
	}
	return nil
}

func (c *fakeConsumer) consumedCount(ref string) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	n := 0
	for _, r := range c.consumed {
		if r == ref {
			n++
		}
	}
	return n
}

func TestRefillAndRoundRobinEvenConsumption(t *testing.T) {
	src := &fakeSource{}
	src.setTasks(
		ShardTask{Shard: 0, Ref: "a"},
		ShardTask{Shard: 0, Ref: "c"},
		ShardTask{Shard: 1, Ref: "b"},
		ShardTask{Shard: 1, Ref: "d"},
	)
	m := NewShardQueueManager(src, &fakeConsumer{}, QueueOptions{ConsumeRate: 1000})
	m.SetAssignment([]int{0, 1}, 1)

	m.refillOnce(context.Background())

	// 轮转序：shard0 / shard1 交替，分片间均匀。
	var order []string
	for {
		ref, ok := m.popNext()
		if !ok {
			break
		}
		order = append(order, ref)
	}
	want := []string{"b", "a", "d", "c"} // rot 从 shard1 起步，两分片交替出队
	if len(order) != len(want) {
		t.Fatalf("pop order = %v, want %v", order, want)
	}
	for i := range want {
		if order[i] != want[i] {
			t.Fatalf("pop order = %v, want %v", order, want)
		}
	}
}

func TestConsumeFailureNotReenqueuedUntilRefill(t *testing.T) {
	src := &fakeSource{}
	src.setTasks(
		ShardTask{Shard: 0, Ref: "bad"},
		ShardTask{Shard: 1, Ref: "good"},
	)
	cons := &fakeConsumer{failRefs: map[string]bool{"bad": true}}
	m := NewShardQueueManager(src, cons, QueueOptions{ConsumeRate: 1000})
	m.SetAssignment([]int{0, 1}, 1)
	ctx := context.Background()

	m.refillOnce(ctx)
	for {
		ref, ok := m.popNext()
		if !ok {
			break
		}
		m.consume(ctx, ref)
	}
	if n := cons.consumedCount("bad"); n != 1 {
		t.Fatalf("'bad' consumed %d times in one pass, want 1", n)
	}
	if _, ok := m.popNext(); ok {
		t.Fatal("failed item was re-enqueued (poison-pill guard violated)")
	}

	// 重灌：DB 行仍在，下一轮自然再拉（失败重试语义归业务）。
	m.refillOnce(ctx)
	if _, ok := m.popNext(); !ok {
		t.Fatal("due row not re-listed after refill")
	}
}

func TestRefillRespectsCapacity(t *testing.T) {
	src := &fakeSource{}
	tasks := make([]ShardTask, 0, 4)
	for _, r := range []string{"a", "b", "c", "d"} {
		tasks = append(tasks, ShardTask{Shard: 0, Ref: r})
	}
	src.setTasks(tasks...)
	m := NewShardQueueManager(src, &fakeConsumer{}, QueueOptions{Capacity: 2, ConsumeRate: 1000})
	m.SetAssignment([]int{0}, 1)
	ctx := context.Background()

	m.refillOnce(ctx)
	if limits := src.limits; len(limits) != 1 || limits[0] != 2 {
		t.Fatalf("first refill limit = %v, want [2]", limits)
	}
	m.mu.Lock()
	qlen := len(m.queues[0])
	m.mu.Unlock()
	if qlen != 2 {
		t.Fatalf("queue length = %d, want capacity 2", qlen)
	}

	// 队满：本轮不再拉取， Due 行留 DB。
	m.refillOnce(ctx)
	if src.callCount() != 1 {
		t.Fatalf("full queue must skip the refill, calls = %d", src.callCount())
	}
}

func TestEpochChangeClearsQueues(t *testing.T) {
	src := &fakeSource{}
	src.setTasks(ShardTask{Shard: 0, Ref: "a"})
	m := NewShardQueueManager(src, &fakeConsumer{}, QueueOptions{ConsumeRate: 1000})
	m.SetAssignment([]int{0}, 1)
	m.refillOnce(context.Background())

	// epoch 变更 → 清空投影，等下一轮重灌。
	m.SetAssignment([]int{0}, 2)
	if _, ok := m.popNext(); ok {
		t.Fatal("queues not cleared on epoch change")
	}
	m.refillOnce(context.Background())
	if _, ok := m.popNext(); !ok {
		t.Fatal("refill after epoch change did not restore items")
	}
}
