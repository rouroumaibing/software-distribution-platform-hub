package models

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"
)

// Trigger types (ui-12 §4.3 / N-21). `manual` needs no row — every POST
// /pipelines/:id/runs is a manual trigger; only scheduled/inbound triggers
// persist configuration.
const (
	TriggerTypeCron    = "cron"
	TriggerTypeWebhook = "webhook"
	TriggerTypeEvent   = "event"
)

// PipelineTrigger is one configured non-manual trigger of a pipeline.
//
// Spec (datatypes.JSON) is per-type:
//   - cron:    {"expr": "*/15 * * * *"}
//   - webhook: {} (token-authed POST fires the run)
//   - event:   {"onPipelineId": "<uuid>", "statuses": ["Succeeded"]}
type PipelineTrigger struct {
	ID         uuid.UUID `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"id"`
	PipelineID uuid.UUID `gorm:"type:uuid;not null;index" json:"pipelineId"`

	Name    string         `gorm:"size:128;not null" json:"name"`
	Type    string         `gorm:"size:16;not null" json:"type"` // cron | webhook | event
	Enabled bool           `gorm:"not null;default:true" json:"enabled"`
	Spec    datatypes.JSON `gorm:"not null;default:'{}'" json:"spec,omitempty"`

	// Token authenticates webhook invocations. Returned once on
	// create/rotate; never echoed back on list/get (only a prefix hint).
	Token string `gorm:"size:128;not null;default:''" json:"-"`

	// Prefix is the first 8 chars of the token — enough for the console to
	// show "wh_3f9a…" without exposing the secret.
	Prefix string `gorm:"-" json:"tokenPrefix,omitempty"`

	LastTriggeredAt *time.Time `json:"lastTriggeredAt,omitempty"`
	CreatedBy       string     `gorm:"size:128" json:"createdBy,omitempty"`

	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

func (PipelineTrigger) TableName() string { return "pipeline_triggers" }

// EventSpec is the parsed shape of an event trigger's Spec.
type EventSpec struct {
	OnPipelineID uuid.UUID `json:"onPipelineId"`
	Statuses     []string  `json:"statuses,omitempty"` // default: Succeeded
}

// DecodeEventSpec parses the stored event Spec body; empty statuses default
// to ["Succeeded"].
func DecodeEventSpec(raw datatypes.JSON) EventSpec {
	var spec EventSpec
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &spec)
	}
	if len(spec.Statuses) == 0 {
		spec.Statuses = []string{"Succeeded"}
	}
	return spec
}

// EncodeEventSpec marshals an EventSpec into the stored JSON shape.
func EncodeEventSpec(spec EventSpec) (datatypes.JSON, error) {
	b, err := json.Marshal(spec)
	return datatypes.JSON(b), err
}
