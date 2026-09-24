package service

import (
	"sync"

	"github.com/google/uuid"

	"github.com/rouroumaibing/software-distribution-platform-hub/internal/target/models"
)

// OpEventKind discriminates the two event shapes an SSE subscriber receives.
const (
	OpEventStatus = "status" // Status carries the op's post-transition state
	OpEventLog    = "log"    // Log carries one streamed output chunk
)

// OpEvent is one unit pushed to an op's SSE subscribers. Status events carry
// the full op (after the transition was applied) so the client can render
// state without a follow-up fetch; log events carry the persisted row (seq
// included) so reconnecting clients can dedupe by seq.
type OpEvent struct {
	Kind   string             `json:"kind"`
	Status *models.AgentOp    `json:"status,omitempty"`
	Log    *models.AgentOpLog `json:"log,omitempty"`
}

// OpStream fans out agent-op events to SSE subscribers.
//
// Concurrency / deployment note (documented assumption, §16.5): subscriptions
// are process-local state, so this fan-out assumes a single hub instance —
// the current deployment shape. Multi-instance hub would need the SSE
// subscribers to fall back to the polling endpoint + persisted log replay
// (both already shipped), which is why the replay-first design was chosen.
type OpStream struct {
	mu   sync.RWMutex
	subs map[uuid.UUID]map[chan OpEvent]struct{}
}

func NewOpStream() *OpStream {
	return &OpStream{subs: make(map[uuid.UUID]map[chan OpEvent]struct{})}
}

// Stream exposes the fan-out hub to the SSE handler (nil when not wired —
// the handler then only replays persisted history).
func (s *AgentOpService) Stream() *OpStream { return s.stream }

// Subscribe registers a channel for an op's events. The returned cancel func
// must be called when the subscriber goes away (defer in the SSE handler).
// The channel is buffered; a slow consumer that lets it fill up drops events
// rather than blocking the Runner's report path — the persisted log replay +
// polling endpoint make dropped chunks recoverable.
func (s *OpStream) Subscribe(opID uuid.UUID) (<-chan OpEvent, func()) {
	ch := make(chan OpEvent, 64)
	s.mu.Lock()
	if s.subs[opID] == nil {
		s.subs[opID] = make(map[chan OpEvent]struct{})
	}
	s.subs[opID][ch] = struct{}{}
	s.mu.Unlock()
	cancel := func() {
		s.mu.Lock()
		if set, ok := s.subs[opID]; ok {
			delete(set, ch)
			if len(set) == 0 {
				delete(s.subs, opID)
			}
		}
		s.mu.Unlock()
	}
	return ch, cancel
}

// Publish fans one event out to every subscriber of the op. Non-blocking:
// a subscriber whose buffer is full loses the event (logged at the handler
// level via the stream closing order, or recovered through replay/polling).
func (s *OpStream) Publish(opID uuid.UUID, ev OpEvent) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for ch := range s.subs[opID] {
		select {
		case ch <- ev:
		default:
		}
	}
}
