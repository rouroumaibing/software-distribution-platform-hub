package models

import (
	"github.com/google/uuid"

	"github.com/rouroumaibing/software-distribution-platform-hub/internal/common"
)

// Service is a business service node in a service tree (e.g. "交易服务","用户中心").
// In the catalog domain it is a tree node under an org's service tree, not an
// application-layer service object. Use ServiceNode in domain-facing code.
type Service struct {
	common.Base
	ServiceTreeID uuid.UUID `gorm:"type:uuid;not null;index" json:"serviceTreeId"`
	Key           string    `gorm:"size:64;not null" json:"key"`
	Name          string    `gorm:"size:128;not null" json:"name"`
	Description   string    `json:"description"`
	OwnerTeam     string    `gorm:"size:128" json:"ownerTeam"`
}

// ServiceNode is the domain-facing name for a catalog Service row. It is an
// alias: the type, table and JSON representation stay identical.
type ServiceNode = Service

func (Service) TableName() string { return "services" }
