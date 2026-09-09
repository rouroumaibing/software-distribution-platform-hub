package models

import (
	"github.com/google/uuid"

	"github.com/rouroumaibing/software-distribution-platform-hub/internal/common"
)

// Component is an independently deployable unit, typically backed by a
// single git repository.
type Component struct {
	common.Base
	ServiceID     uuid.UUID `gorm:"type:uuid;not null;index" json:"serviceId"`
	Key           string    `gorm:"size:64;not null" json:"key"`
	Name          string    `gorm:"size:128;not null" json:"name"`
	RepoURL       string    `gorm:"size:512;not null" json:"repoUrl"`
	DefaultBranch string    `gorm:"size:128;not null;default:main" json:"defaultBranch"`
	RepoSecretRef string    `gorm:"size:128" json:"repoSecretRef,omitempty"`
	Language      string    `gorm:"size:64" json:"language"`
	Description   string    `json:"description"`
}

func (Component) TableName() string { return "components" }
