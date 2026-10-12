package models

import (
	"time"

	"github.com/google/uuid"
)

// ComponentDependency is a directed "A depends on B" edge (ui-12 §6.1 依赖图).
// DependsOnID = 被依赖方（upstream）。环在写入时拒绝（AddDependency 前置 DFS）。
type ComponentDependency struct {
	ID          uuid.UUID `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"id"`
	ComponentID uuid.UUID `gorm:"type:uuid;not null;uniqueIndex:idx_dep_pair,priority:1" json:"componentId"`
	DependsOnID uuid.UUID `gorm:"type:uuid;not null;uniqueIndex:idx_dep_pair,priority:2" json:"dependsOnId"`
	CreatedAt   time.Time `json:"createdAt"`
}

func (ComponentDependency) TableName() string { return "component_dependencies" }
