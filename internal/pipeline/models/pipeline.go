package models

import (
	"github.com/google/uuid"

	"github.com/rouroumaibing/software-distribution-platform-hub/internal/common"
)

// Pipeline is a definition-level template (e.g. "构建流水线","发布流水线").
type Pipeline struct {
	common.Base
	// 名字唯一性只作用于"未软删"的行。pipeline 是软删的（common.Base），所以一个
	// 不含 deleted_at 的 unique(component_id, name) 会让删掉的流水线把名字永久占住
	// ——再建同名流水线直接唯一键冲突 500。partial unique index 让已删流水线的名字
	// 可被复用，同时仍禁止同一组件下出现两条同名活流水线。
	// DDL 侧同义声明见 migrations/0001_init_schema.sql 与 0006_pipeline_active_uniqueness.sql。
	ComponentID uuid.UUID `gorm:"type:uuid;not null;index;uniqueIndex:idx_pipelines_component_name_active,where:deleted_at IS NULL" json:"componentId"`
	Name        string    `gorm:"size:128;not null;uniqueIndex:idx_pipelines_component_name_active,where:deleted_at IS NULL" json:"name"`
	Kind        string    `gorm:"size:32;not null;default:custom" json:"kind"` // build/release/custom
	Description string    `json:"description"`
	CreatedBy   string    `gorm:"size:128" json:"createdBy,omitempty"`

	// Version increments on every structural edit; a PipelineRun records
	// which version it was triggered with.
	Version int `gorm:"not null;default:1" json:"version"`
}

func (Pipeline) TableName() string { return "pipelines" }
