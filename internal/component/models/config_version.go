package models

import (
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"
)

// ConfigSnapshot is an immutable whole-environment configuration version
// (ui-12 §4.1/§4.2, N-18/N-19). Every config mutation (upsert / delete) and
// every rollback captures the full set of effective config rows for one scope
// — global defaults (environmentID == nil) or a single environment — as a new
// version. The model mirrors PipelineVersion: append-only, monotonic per
// scope, snapshot body kept as JSON so a rollback re-materializes the exact
// bytes that were captured.
//
// Versioning runs on the same (component, environment-scope) axis the config
// CRUD already uses: a global-default edit only bumps the global version, an
// environment override only bumps that environment's version.
type ConfigSnapshot struct {
	ID            uuid.UUID  `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"id"`
	ComponentID   uuid.UUID  `gorm:"type:uuid;not null;uniqueIndex:idx_config_snapshots_scope_version" json:"componentId"`
	EnvironmentID *uuid.UUID `gorm:"type:uuid" json:"environmentId,omitempty"`
	// ScopeKey makes the per-scope monotonic version enforceable in Postgres:
	// a unique index treats NULLs as distinct, so the raw environment_id
	// cannot carry the constraint for global-default snapshots. It holds the
	// environment uuid or "" for the global scope.
	ScopeKey  string         `gorm:"size:64;not null;default:'';uniqueIndex:idx_config_snapshots_scope_version" json:"-"`
	Version   int            `gorm:"not null;uniqueIndex:idx_config_snapshots_scope_version" json:"version"`
	Comment   string         `gorm:"size:512" json:"comment,omitempty"`
	Snapshot  datatypes.JSON `gorm:"not null" json:"snapshot"`
	CreatedBy *string        `gorm:"size:128" json:"createdBy,omitempty"`
	CreatedAt time.Time      `json:"createdAt"`
}

func (ConfigSnapshot) TableName() string { return "component_config_snapshots" }

// ScopeKeyOf renders the scope key for a (component-local) environment scope.
func ScopeKeyOf(environmentID *uuid.UUID) string {
	if environmentID == nil {
		return ""
	}
	return environmentID.String()
}

// ConfigSnapshotEntry is one config key inside a snapshot body.
type ConfigSnapshotEntry struct {
	Key       string `json:"key"`
	Value     string `json:"value,omitempty"`
	IsSecret  bool   `json:"isSecret"`
	SecretRef string `json:"secretRef,omitempty"`
}

// BuildConfigSnapshot assembles the snapshot body from the current config
// rows (ordered by key for byte-stable output). Pure — no DB, no clock — so
// the shape it produces is unit-testable on its own. Secret values are masked
// here (N-18: history must never carry plaintext secrets).
func BuildConfigSnapshot(rows []ComponentConfig) []ConfigSnapshotEntry {
	entries := make([]ConfigSnapshotEntry, 0, len(rows))
	for _, r := range rows {
		e := ConfigSnapshotEntry{
			Key:       r.Key,
			IsSecret:  r.IsSecret,
			SecretRef: r.SecretRef,
		}
		if r.IsSecret {
			e.Value = "<secret>"
		} else {
			e.Value = r.Value
		}
		entries = append(entries, e)
	}
	sort.SliceStable(entries, func(i, j int) bool { return entries[i].Key < entries[j].Key })
	return entries
}

// EncodeConfigSnapshot serializes the snapshot body.
func EncodeConfigSnapshot(entries []ConfigSnapshotEntry) (datatypes.JSON, error) {
	raw, err := json.Marshal(entries)
	if err != nil {
		return nil, fmt.Errorf("encode config snapshot: %w", err)
	}
	return datatypes.JSON(raw), nil
}

// DecodeConfigSnapshot parses a stored snapshot body.
func DecodeConfigSnapshot(raw datatypes.JSON) ([]ConfigSnapshotEntry, error) {
	var entries []ConfigSnapshotEntry
	if len(raw) == 0 {
		return entries, nil
	}
	if err := json.Unmarshal(raw, &entries); err != nil {
		return nil, fmt.Errorf("decode config snapshot: %w", err)
	}
	return entries, nil
}

// ---------- diff（与 PipelineVersion 同构：纯函数，键为自然键） ----------

// ConfigChangeType enumerates how a key differs between two versions.
type ConfigChangeType string

const (
	ConfigChangeAdded    ConfigChangeType = "added"
	ConfigChangeRemoved  ConfigChangeType = "removed"
	ConfigChangeModified ConfigChangeType = "modified"
)

// ConfigKeyDiff is one key-level difference, rendered as strings so the
// console can print a diff row without extra type knowledge. Secret values
// stay masked ("<secret>") in From/To.
type ConfigKeyDiff struct {
	Key       string           `json:"key"`
	Change    ConfigChangeType `json:"change"`
	FromValue string           `json:"from,omitempty"`
	ToValue   string           `json:"to,omitempty"`
}

// ConfigSnapshotDiff is the result of comparing two config versions.
type ConfigSnapshotDiff struct {
	FromVersion   int             `json:"fromVersion"`
	ToVersion     int             `json:"toVersion"`
	FromCreatedAt time.Time       `json:"fromCreatedAt"`
	ToCreatedAt   time.Time       `json:"toCreatedAt"`
	Keys          []ConfigKeyDiff `json:"keys"`
	Summary       struct {
		Added    int `json:"added"`
		Removed  int `json:"removed"`
		Modified int `json:"modified"`
	} `json:"summary"`
	Identical bool `json:"identical"`
}

// DiffConfigSnapshots compares two snapshot bodies (from → to). Pure — no DB,
// no clock. Identity is the config key (the API's natural key). Secrets that
// only change SecretRef count as modified via the ref; two "<secret>" values
// never leak whether the underlying ciphertext differs.
func DiffConfigSnapshots(from, to []ConfigSnapshotEntry, fromVersion, toVersion int, fromAt, toAt time.Time) ConfigSnapshotDiff {
	d := ConfigSnapshotDiff{
		FromVersion:   fromVersion,
		ToVersion:     toVersion,
		FromCreatedAt: fromAt,
		ToCreatedAt:   toAt,
		Keys:          []ConfigKeyDiff{},
	}
	fromIdx := indexEntries(from)
	toIdx := indexEntries(to)

	for _, e := range to {
		prev, ok := fromIdx[e.Key]
		switch {
		case !ok:
			d.Keys = append(d.Keys, ConfigKeyDiff{Key: e.Key, Change: ConfigChangeAdded, ToValue: e.Value})
			d.Summary.Added++
		default:
			fields := entryFields(prev, e)
			if len(fields) > 0 {
				d.Keys = append(d.Keys, ConfigKeyDiff{
					Key:       e.Key,
					Change:    ConfigChangeModified,
					FromValue: fields[0].From,
					ToValue:   fields[0].To,
				})
				d.Summary.Modified++
			}
		}
	}
	for _, e := range from {
		if _, ok := toIdx[e.Key]; !ok {
			d.Keys = append(d.Keys, ConfigKeyDiff{Key: e.Key, Change: ConfigChangeRemoved, FromValue: e.Value})
			d.Summary.Removed++
		}
	}
	sort.SliceStable(d.Keys, func(i, j int) bool { return d.Keys[i].Key < d.Keys[j].Key })
	d.Identical = len(d.Keys) == 0
	return d
}

// entryFields returns the value-level changes between two entries. A secret's
// Value is already masked ("<secret>") by BuildConfigSnapshot, so ciphertext
// never flows into a diff; what a reviewer can see is the ref change.
func entryFields(from, to ConfigSnapshotEntry) []struct{ From, To string } {
	var out []struct{ From, To string }
	if from.Value != to.Value {
		out = append(out, struct{ From, To string }{from.Value, to.Value})
	}
	if from.SecretRef != to.SecretRef {
		out = append(out, struct{ From, To string }{from.SecretRef, to.SecretRef})
	}
	return out
}

func indexEntries(entries []ConfigSnapshotEntry) map[string]ConfigSnapshotEntry {
	out := make(map[string]ConfigSnapshotEntry, len(entries))
	for _, e := range entries {
		out[e.Key] = e
	}
	return out
}
