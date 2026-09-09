package models

import (
	"github.com/google/uuid"

	"github.com/rouroumaibing/software-distribution-platform-hub/internal/common"
)

// Service is a business service grouping (e.g. "交易服务","用户中心").
type Service struct {
	common.Base
	ServiceTreeID uuid.UUID `gorm:"type:uuid;not null;index" json:"serviceTreeId"`
	Key           string    `gorm:"size:64;not null" json:"key"`
	Name          string    `gorm:"size:128;not null" json:"name"`
	Description   string    `json:"description"`
	OwnerTeam     string    `gorm:"size:128" json:"ownerTeam"`
}

func (Service) TableName() string { return "services" }
