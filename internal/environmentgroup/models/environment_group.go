package models

import (
	"github.com/google/uuid"

	"github.com/rouroumaibing/software-distribution-platform-hub/internal/common"
)

// EnvironmentGroup is a user-defined grouping of environments under a component
// (DATA-MODEL §8). Grouping is orthogonal to env type; deletion is only allowed
// when the group is empty (§7.3).
type EnvironmentGroup struct {
	common.BaseNoSoftDelete
	ComponentID uuid.UUID `gorm:"type:uuid;not null;index" json:"componentId"`
	Name        string    `gorm:"size:128;not null" json:"name"`
	Description string    `gorm:"size:256" json:"description,omitempty"`
	OrderIndex  int       `gorm:"not null;default:0" json:"orderIndex"`
}

func (EnvironmentGroup) TableName() string { return "environment_groups" }
