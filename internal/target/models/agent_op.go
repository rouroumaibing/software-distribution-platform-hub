package models

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// Agent operation types (§9.9 接入编排 / §9.5 直连执行).
const (
	AgentOpExec    = "exec"    // 在环境所属目标上执行命令/脚本（直连通道）
	AgentOpInstall = "install" // 向目标安装 runner（版本取版本矩阵 §9.10）
	AgentOpUpgrade = "upgrade" // 把已装 runner 升到版本矩阵里的版本
)

// Agent operation lifecycle. `queued` is written by the hub at Create; the
// Runner moves the op forward via agent_op_status reports (2026-09-23 第十八批,
// UNIMPLEMENTED-MODULES-PLAN §16.5): queued→running when it picks the op up,
// running→succeeded|failed on completion. Transitions are validated by
// models.IsValidAgentOpTransition and terminal states are immutable. Ops that
// stay queued are either waiting for the target's Runner to connect (drained
// on reconnect) or — for install/upgrade — waiting on the bootstrap flow
// (§9.9 引导执行器属独立特性).
const (
	AgentOpQueued    = "queued"
	AgentOpRunning   = "running"
	AgentOpSucceeded = "succeeded"
	AgentOpFailed    = "failed"
)

// IsValidAgentOpType guards the op-type enum at the service boundary.
func IsValidAgentOpType(t string) bool {
	switch t {
	case AgentOpExec, AgentOpInstall, AgentOpUpgrade:
		return true
	}
	return false
}

// IsValidAgentOpTransition reports whether the Runner may move an op from
// `from` to `to`. Forward-only; terminal states (succeeded / failed) never
// change again so the audit trail stays trustworthy.
func IsValidAgentOpTransition(from, to string) bool {
	switch from {
	case AgentOpQueued:
		return to == AgentOpRunning || to == AgentOpSucceeded || to == AgentOpFailed
	case AgentOpRunning:
		return to == AgentOpSucceeded || to == AgentOpFailed
	default: // succeeded / failed / unknown
		return false
	}
}

// IsTerminalAgentOpStatus reports whether a status ends an op's lifecycle.
func IsTerminalAgentOpStatus(s string) bool {
	return s == AgentOpSucceeded || s == AgentOpFailed
}

// IsValidAgentOpStatus guards the status enum at the ApplyStatus boundary
// (the Runner may only report these three; queued is hub-internal).
func IsValidAgentOpStatus(s string) bool {
	switch s {
	case AgentOpRunning, AgentOpSucceeded, AgentOpFailed:
		return true
	}
	return false
}

// AgentOp is one hub-issued operation targeting a Runner agent. The hub has no
// client-go: it never talks to a target cluster itself, so an operation is a
// durable, auditable record of "hub asked the agent to do X" rather than a
// synchronous RPC.
type AgentOp struct {
	ID       uuid.UUID  `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"id"`
	TargetID uuid.UUID  `gorm:"type:uuid;not null;index" json:"targetId"`
	EnvID    *uuid.UUID `gorm:"type:uuid" json:"envId,omitempty"`
	OpType   string     `gorm:"size:16;not null" json:"opType"`
	Status   string     `gorm:"size:16;not null;default:queued" json:"status"`
	// Detail carries the op-specific argument: the command (or script) for
	// exec, the runner version for install/upgrade. text (not size:1024) —
	// exec scripts regularly exceed 1KiB and the dispatch payload must carry
	// the command verbatim (migration 0018 widened the column; AutoMigrate
	// does not widen existing columns).
	Detail  string `gorm:"type:text" json:"detail,omitempty"`
	Message string `gorm:"size:512" json:"message,omitempty"`

	CreatedAt time.Time      `json:"createdAt"`
	UpdatedAt time.Time      `json:"updatedAt"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`
}

func (AgentOp) TableName() string { return "agent_ops" }

// AgentOpLog is one streamed output chunk produced by a dispatched agent op.
// Chunks are persisted as they arrive so an SSE subscriber that connects late
// (or reconnects) can replay the full output, and so the output survives the
// Runner pod going away — the same replay-after-the-fact contract the run log
// store (TaskRunLog) gives pipeline runs.
type AgentOpLog struct {
	ID   uuid.UUID `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"id"`
	OpID uuid.UUID `gorm:"type:uuid;not null;index" json:"opId"`
	// Seq orders chunks within one op (monotonic per op, assigned by the hub
	// as chunks arrive — the Runner makes no ordering guarantees across
	// goroutines).
	Seq    int    `gorm:"not null" json:"seq"`
	Stream string `gorm:"size:16" json:"stream,omitempty"` // stdout | stderr
	Chunk  string `gorm:"type:text" json:"chunk"`

	CreatedAt time.Time `json:"createdAt"`
}

func (AgentOpLog) TableName() string { return "agent_op_logs" }
