package models

import (
	"github.com/google/uuid"

	"github.com/rouroumaibing/software-distribution-platform-hub/internal/common"
)

// ExecutionMode 合法取值（stage 级：一个阶段统一一种模式）。与
// hub/API-REFERENCE.md 的 `stages[].executionMode`、console 编排页原型一致，
// 一律小写。
const (
	ExecutionModeParallel = "parallel" // 默认：阶段内子任务并发启动，互不等待
	ExecutionModeSerial   = "serial"   // 阶段内子任务严格先后
)

// IsValidExecutionMode 判断取值是否在合法集合内。空串**不算合法**——
// "缺省"与"非法"是两回事，调用方各自处理（Create 落默认、Update 视为不改）。
func IsValidExecutionMode(m string) bool {
	return m == ExecutionModeParallel || m == ExecutionModeSerial
}

// PipelineStage groups task templates that run in parallel; stages
// themselves execute in Sequence order.
//
// 软删（common.Base）而不是硬删：pipeline 本身是软删的（run 历史必须比定义活得
// 久，见 common/base.go），若结构是硬删，恢复一条流水线只会得到空壳 —— 一个既非
// 纯软删、也非纯硬删的"半状态"（DELETE-CONTRACT §6.6-3 决策 3）。
// DDL 上的 `on delete cascade` 因两侧都改为软删而永不触发，因此删 stage 时由
// StageService 显式软删其下任务模板（见 service/stage.go 的 TemplateCascade）。
type PipelineStage struct {
	common.Base
	// 唯一性只作用于未软删的行：否则删掉一个 stage 后，同 pipeline 下再建同
	// sequence 的 stage 会撞 0001 里的 `unique (pipeline_id, sequence)` → 直接
	// 500。partial unique index 让已删 stage 的 sequence 可被复用，同时仍禁止同一
	// pipeline 下出现两个同 sequence 的活阶段。
	// 索引由 PipelineID + Sequence **两个字段共同声明**（与 pipelines 的
	// idx_pipelines_component_name_active 同一手法），列序即 struct 字段序；
	// DDL 侧同义声明见 migrations/0001_init_schema.sql 与 0009。
	PipelineID uuid.UUID `gorm:"type:uuid;not null;index;uniqueIndex:idx_stages_pipeline_seq_active,where:deleted_at IS NULL" json:"pipelineId"`
	Name       string    `gorm:"size:128;not null" json:"name"`
	Sequence   int       `gorm:"not null;uniqueIndex:idx_stages_pipeline_seq_active,where:deleted_at IS NULL" json:"sequence"`

	// ExecutionMode：阶段内子任务并行 / 串行。
	//
	// 契约与 DDL 早已拍板（hub/API-REFERENCE.md 的 `stages[].executionMode` →
	// `pipeline_stages.execution_mode`；hub/DATA-MODEL.md §6.4-① 连 ALTER 语句都写好
	// 了），但 hub 结构体此前**没有这个字段**（DATA-MODEL 自己标注「⚠️ 当前未实现」）。
	// 结果是 console 编排页的阶段「并行/串行」开关无处落库 —— 只能做成刷新即失效的
	// 装饰控件。本字段补的是"存得下、读得回"这一半，让开关变成真数据。
	//
	// 边界（2026-09-23 起完整）：Serial 的**调度行为已落地** —— hub 侧 buildSpec
	// （internal/run/service）为 serial 阶段内任务按模板顺序派生「紧邻前驱」
	// DependsOn 链（跨阶段推导照旧叠加；调用方手写 DependsOn 完全优先），runner
	// 按依赖 DAG 执行即呈先后串行；parallel 阶段不派生阶段内依赖（backlog C-06
	// 闭口，单测见 pipeline_run_serial_test.go）。
	//
	// 取值小写（parallel/serial）与 API 契约和原型对齐；DATA-MODEL §6.4-① 的草案
	// ALTER 用的是 'Parallel'（首字母大写），本次以**面向客户端的 API 契约**为准统一
	// 为小写，该文档已同步回填。DDL 侧见 migrations/0010。
	ExecutionMode string `gorm:"size:16;not null;default:parallel" json:"executionMode"`
}

func (PipelineStage) TableName() string { return "pipeline_stages" }
