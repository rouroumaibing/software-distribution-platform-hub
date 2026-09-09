package models

import (
	"github.com/google/uuid"

	"github.com/rouroumaibing/software-distribution-platform-hub/internal/common"
)

// ServiceTree is 1:1 with Org; created automatically when an Org is created.
type ServiceTree struct {
	common.BaseNoSoftDelete
	OrgID uuid.UUID `gorm:"type:uuid;not null;uniqueIndex" json:"orgId"`
	Name  string    `gorm:"size:128;not null;default:default" json:"name"`
}

func (ServiceTree) TableName() string { return "service_trees" }
