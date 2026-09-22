package models

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"
)

// ComponentRole is a component-scoped role (ArgoCD Project-scoped RBAC,
// see DATA-MODEL §7.3).
//
// OrgID == nil  → built-in (component-viewer / component-editor /
//
//	component-approver / component-admin, seeded).
//
// OrgID != nil  → org-defined custom role (B-11 自定义角色).
//
// 唯一性拆成两条 **partial** unique index（`migrations/0014`）：
//   - 自定义角色：(org_id, name) 在**所属组织内**唯一 —— 两个组织各自定义
//     "release-owner" 是合理的；
//   - 内置角色：name 在**全局**唯一 —— `GetByName` 按名字解析内置角色
//     （owner 引导 / 绑定校验），同名即二义。
//
// 不能写成 `unique (org_id, name)` 一条了事：Postgres 里 NULL 互不相等，
// 内置角色（org_id is null）会**完全不受约束**，可以插进任意多条同名行。
type ComponentRole struct {
	ID          uuid.UUID      `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"id"`
	OrgID       *uuid.UUID     `gorm:"type:uuid;index;uniqueIndex:idx_component_roles_org_name,where:org_id IS NOT NULL" json:"orgId,omitempty"`
	Name        string         `gorm:"size:64;not null;uniqueIndex:idx_component_roles_org_name,where:org_id IS NOT NULL;uniqueIndex:idx_component_roles_builtin_name,where:org_id IS NULL" json:"name"`
	Description string         `json:"description,omitempty"`
	Actions     datatypes.JSON `gorm:"not null;default:'[]'" json:"actions"`
	IsSystem    bool           `gorm:"not null;default:false" json:"isSystem"`
	CreatedAt   time.Time      `json:"createdAt"`
}

func (ComponentRole) TableName() string { return "component_roles" }
