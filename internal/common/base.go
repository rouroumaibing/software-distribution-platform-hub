package common

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// Base is embedded by tables that support soft delete: orgs, services,
// components, pipelines, credentials, pipeline_stages and
// pipeline_task_templates. Deleting a row here should never cascade-delete
// run history, so these use GORM's soft delete instead of a hard DELETE.
//
// (The local `users` table used to be in this list; D3 removed it along with
// the per-request provisioning — hub no longer persists identities at all.)
//
// 软删表若带唯一约束，必须写成 `where deleted_at is null` 的 partial unique
// index（pipelines / pipeline_stages / pipeline_task_templates 同此手法）：
// 一条不含 deleted_at 的 unique 会让已删行把名字永久占住，重建同名直接 500。
type Base struct {
	ID        uuid.UUID      `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"id"`
	CreatedAt time.Time      `json:"createdAt"`
	UpdatedAt time.Time      `json:"updatedAt"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`
}

// BaseNoSoftDelete is embedded by tables without soft delete: targets,
// environments, environment_groups, service_trees, and every run history
// table. These are hard-deleted, so a table here that can be orphaned by a
// soft-deleted parent has no self-marking column — its queries must join the
// parent and filter on the parent's deleted_at.
type BaseNoSoftDelete struct {
	ID        uuid.UUID `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"id"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}
