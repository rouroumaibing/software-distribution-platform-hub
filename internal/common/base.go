package common

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// Base is embedded by tables that support soft delete: orgs, services,
// components, pipelines. Deleting a row here should never cascade-delete
// run history, so these use GORM's soft delete instead of a hard DELETE.
type Base struct {
	ID        uuid.UUID      `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"id"`
	CreatedAt time.Time      `json:"createdAt"`
	UpdatedAt time.Time      `json:"updatedAt"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`
}

// BaseNoSoftDelete is embedded by tables without soft delete: clusters,
// environments, pipeline_stages, pipeline_task_templates, and every run
// history table.
type BaseNoSoftDelete struct {
	ID        uuid.UUID `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"id"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}
