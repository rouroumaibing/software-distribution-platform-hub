package models

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"
)

// VariableGroup is an org-scoped set of shared plaintext variables (ui-12
// §5.3, N-26). Components reference a group via ComponentVariableGroupBinding;
// at trigger time bound entries merge into run params below component
// variables (resolution: run inputs > component vars > group vars). Group
// entries are plaintext only — secrets stay in credentials (AES-GCM), never
// duplicated into a shared group.
//
// The model lives in the component package (not org) because its consumer —
// the binding table and the trigger-time resolver — is component-scoped, and
// the component package must not import org models (same reason EnvKeyResolver
// is a one-method interface).
type VariableGroup struct {
	ID          uuid.UUID `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"id"`
	OrgID       uuid.UUID `gorm:"type:uuid;not null;index" json:"orgId"`
	Name        string    `gorm:"size:128;not null;uniqueIndex:idx_variable_groups_org_name" json:"name"`
	Description string    `json:"description"`
	// Variables is the JSON body: []VariableEntry{key,value,description}.
	Variables datatypes.JSON `json:"variables"`
	CreatedBy string         `gorm:"size:128" json:"createdBy,omitempty"`
	CreatedAt time.Time      `json:"createdAt"`
	UpdatedAt time.Time      `json:"updatedAt"`
}

func (VariableGroup) TableName() string { return "variable_groups" }

// VariableEntry is one key/value pair inside a VariableGroup body.
type VariableEntry struct {
	Key         string `json:"key"`
	Value       string `json:"value,omitempty"`
	Description string `json:"description,omitempty"`
}

// ComponentVariableGroupBinding links a component to a shared variable group.
// Priority orders the merge when several groups define the same key: lower
// number wins first (later groups override earlier ones at equal semantics —
// the resolver sorts ascending and applies in order so the highest priority
// entry lands last).
type ComponentVariableGroupBinding struct {
	ID          uuid.UUID `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"id"`
	ComponentID uuid.UUID `gorm:"type:uuid;not null;index;uniqueIndex:idx_var_bindings_component_group" json:"componentId"`
	GroupID     uuid.UUID `gorm:"type:uuid;not null;uniqueIndex:idx_var_bindings_component_group" json:"groupId"`
	Priority    int       `gorm:"not null;default:100" json:"priority"`
	CreatedBy   string    `gorm:"size:128" json:"createdBy,omitempty"`
	CreatedAt   time.Time `json:"createdAt"`
}

func (ComponentVariableGroupBinding) TableName() string { return "component_variable_group_bindings" }

// GroupVariableParams flattens groups (already ordered by ascending priority —
// the resolver applies them in order so later = stronger) into trigger params.
// Pure: no DB, no clock. Duplicate keys resolve to the LAST occurrence, which
// is the binding the resolver wanted to win.
func GroupVariableParams(groups []VariableGroup) []struct {
	Name  string
	Value string
} {
	seen := map[string]int{}
	var out []struct {
		Name  string
		Value string
	}
	for _, g := range groups {
		var entries []VariableEntry
		if len(g.Variables) > 0 {
			_ = json.Unmarshal(g.Variables, &entries) // bad body degrades to empty, never panics
		}
		for _, e := range entries {
			if e.Key == "" {
				continue
			}
			if i, ok := seen[e.Key]; ok {
				out[i].Value = e.Value
				continue
			}
			seen[e.Key] = len(out)
			out = append(out, struct {
				Name  string
				Value string
			}{e.Key, e.Value})
		}
	}
	return out
}
