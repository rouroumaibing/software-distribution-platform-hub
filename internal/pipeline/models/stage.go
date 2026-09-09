package models

import (
	"github.com/google/uuid"

	"github.com/rouroumaibing/software-distribution-platform-hub/internal/common"
)

// PipelineStage groups task templates that run in parallel; stages
// themselves execute in Sequence order.
type PipelineStage struct {
	common.BaseNoSoftDelete
	PipelineID uuid.UUID `gorm:"type:uuid;not null;index" json:"pipelineId"`
	Name       string    `gorm:"size:128;not null" json:"name"`
	Sequence   int       `gorm:"not null" json:"sequence"`
}

func (PipelineStage) TableName() string { return "pipeline_stages" }
