package service

import (
	"context"
	"fmt"
	"net/http"

	"github.com/google/uuid"

	"github.com/rouroumaibing/software-distribution-platform-hub/internal/common"
	applog "github.com/rouroumaibing/software-distribution-platform-hub/internal/common/logger"
	"github.com/rouroumaibing/software-distribution-platform-hub/internal/target/models"
)

// errAgentOpTransition is returned when a Runner reports a status the ledger
// does not allow (rewind, or a write after a terminal state). 409 keeps the
// audit trail trustworthy: a once-succeeded op must never turn into failed.
var errAgentOpTransition = common.NewAPIError(common.KindTarget, http.StatusConflict, 91, "Agent Op State Conflict")

// AgentOpStore is the narrow persistence surface for agent operations. The
// repo convention that keeps this service unit-testable without Postgres;
// *repository.AgentOpRepository satisfies it implicitly.
type AgentOpStore interface {
	Create(*models.AgentOp) error
	GetByID(id uuid.UUID) (*models.AgentOp, error)
	ListByTarget(targetID uuid.UUID, p common.Pagination) ([]models.AgentOp, int64, error)
	// Lifecycle / streaming surface (2026-09-23 第十八批 §16.5).
	UpdateStatus(id uuid.UUID, from, to, message string) error
	AppendLog(opID uuid.UUID, stream, chunk string) (*models.AgentOpLog, error)
	ListLogs(opID uuid.UUID) ([]models.AgentOpLog, error)
	ListQueuedByTarget(targetID uuid.UUID) ([]models.AgentOp, error)
}

// OpDispatcher pushes a queued op down the target's live gateway connection.
// Implemented by the main.go wiring: it assembles the wire payload (env
// namespace, decrypted kubeconfig credential for kubeconfig-access envs) and
// calls gateway.HubServer.DispatchAgentOp. Returning gateway.ErrNoRunner is
// normal — the op simply stays queued until the Runner connects (drained by
// DrainTarget). install/upgrade are NOT dispatched: their executors depend on
// the §9.9 bootstrap flow (enroll-token credential handover), a separate
// feature; the dispatcher leaves them queued by design.
type OpDispatcher interface {
	Dispatch(op *models.AgentOp) error
}

// AgentOpService records hub-issued agent operations and drives their
// lifecycle: the hub validates + records + dispatches (§9.9 编排语义：hub 承担
// 编排，runner 承担执行 — hub 无 client-go).
type AgentOpService struct {
	repo       AgentOpStore
	dispatcher OpDispatcher
	stream     *OpStream
}

func NewAgentOpService(repo AgentOpStore) *AgentOpService {
	return &AgentOpService{repo: repo}
}

// SetDispatcher wires the gateway dispatch hook (optional — unit tests run
// dispatcher-less and ops stay queued).
func (s *AgentOpService) SetDispatcher(d OpDispatcher) { s.dispatcher = d }

// SetStream wires the SSE fan-out hub (optional — nil simply skips publishing).
func (s *AgentOpService) SetStream(st *OpStream) { s.stream = st }

// Create validates and persists an operation, then hands exec ops to the
// dispatcher. Op type is the only caller-controlled enum; status always
// starts at queued. A dispatch failure (offline target) is NOT a request
// failure: the 202 handle stands and the op drains on Runner connect.
func (s *AgentOpService) Create(op *models.AgentOp) error {
	if !models.IsValidAgentOpType(op.OpType) {
		return common.ErrBadRequest.WithError(fmt.Errorf("unknown agent op type %q", op.OpType))
	}
	if op.TargetID == uuid.Nil {
		return common.ErrBadRequest.WithError(fmt.Errorf("agent op requires a target"))
	}
	op.Status = models.AgentOpQueued
	if err := s.repo.Create(op); err != nil {
		return err
	}
	s.tryDispatch(op)
	return nil
}

// tryDispatch best-effort pushes the op to the target's Runner. Only exec is
// dispatched (§16.5 裁定)：install/upgrade 的执行器依赖 §9.9 bootstrap 流程
// （enroll-token 凭据流转给尚无 runner 的目标），独立特性，留守 queued。
func (s *AgentOpService) tryDispatch(op *models.AgentOp) {
	if s.dispatcher == nil || op.OpType != models.AgentOpExec {
		return
	}
	if err := s.dispatcher.Dispatch(op); err != nil {
		// Offline target is the common, expected case; anything else is
		// logged so a wiring bug cannot silently strand ops as queued.
		applog.Infof("agentop: dispatch deferred for op %s (%s): %v", op.ID, op.OpType, err)
	}
}

// ApplyStatus applies one Runner-reported lifecycle transition and fans the
// resulting op state out to SSE subscribers. Illegal transitions are rejected
// with 409 and never touch the row.
func (s *AgentOpService) ApplyStatus(_ context.Context, opID uuid.UUID, to, message string) (*models.AgentOp, error) {
	if !models.IsValidAgentOpStatus(to) {
		return nil, common.ErrBadRequest.WithError(fmt.Errorf("unknown agent op status %q", to))
	}
	op, err := s.repo.GetByID(opID)
	if err != nil {
		return nil, err
	}
	if !models.IsValidAgentOpTransition(op.Status, to) {
		return nil, errAgentOpTransition.WithError(
			fmt.Errorf("cannot transition op %s from %s to %s", opID, op.Status, to))
	}
	if err := s.repo.UpdateStatus(op.ID, op.Status, to, message); err != nil {
		return nil, err
	}
	op.Status = to
	op.Message = message
	s.publishOp(op)
	return op, nil
}

// AppendLog persists one streamed output chunk and fans it out to SSE
// subscribers. The op must exist (404 otherwise) so a bogus opID from a
// misbehaving Runner cannot grow orphan rows.
func (s *AgentOpService) AppendLog(_ context.Context, opID uuid.UUID, stream, chunk string) (*models.AgentOpLog, error) {
	if _, err := s.repo.GetByID(opID); err != nil {
		return nil, err
	}
	row, err := s.repo.AppendLog(opID, stream, chunk)
	if err != nil {
		return nil, err
	}
	if s.stream != nil {
		s.stream.Publish(opID, OpEvent{Kind: OpEventLog, Log: row})
	}
	return row, nil
}

// Logs returns an op's persisted chunks in arrival order (SSE replay source
// and the polling fallback's output history).
func (s *AgentOpService) Logs(opID uuid.UUID) ([]models.AgentOpLog, error) {
	if _, err := s.repo.GetByID(opID); err != nil {
		return nil, err
	}
	return s.repo.ListLogs(opID)
}

// DrainTarget re-dispatches the ops enqueued while the target's Runner was
// offline. Invoked from the gateway connect hook, alongside the run service's
// DrainTarget.
func (s *AgentOpService) DrainTarget(ctx context.Context, targetID uuid.UUID) error {
	ops, err := s.repo.ListQueuedByTarget(targetID)
	if err != nil {
		return err
	}
	for i := range ops {
		s.tryDispatch(&ops[i])
	}
	return nil
}

func (s *AgentOpService) publishOp(op *models.AgentOp) {
	if s.stream != nil {
		s.stream.Publish(op.ID, OpEvent{Kind: OpEventStatus, Status: op})
	}
}

func (s *AgentOpService) Get(id uuid.UUID) (*models.AgentOp, error) {
	return s.repo.GetByID(id)
}

func (s *AgentOpService) ListByTarget(targetID uuid.UUID, p common.Pagination) ([]models.AgentOp, int64, error) {
	return s.repo.ListByTarget(targetID, p)
}
