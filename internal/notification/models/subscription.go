package models

import (
	"strings"
	"time"
)

// Subscription is a per-subject notification subscription (ui-12 §5.2). hub
// keeps no user table (D3), so the email address is declared BY the subject —
// it is the destination the subject typed in, not a directory lookup.
//
// 决策 #5 收敛（2026-10-12 用户拍板）：本期渠道只做「邮件」；站内通知中心已存在
// （聚合型、无需订阅），企业 IM/Webhook 渠道留作后续适配器插件。
type Subscription struct {
	// Subject is the authenticated principal (Keycloak sub / dev user) —
	// same stable identity every binding keys on.
	Subject string `gorm:"size:256;primaryKey" json:"subject"`

	// Email is the destination address for email-channel deliveries. Empty =
	// 站内 only（不发邮件）.
	Email string `gorm:"size:256" json:"email"`

	// Events is the comma-separated event-type whitelist. Supported values:
	//   approval_required / run_finished / run_failed
	// Empty defaults to "approval_required" (the action-item baseline).
	Events string `gorm:"size:256;not null;default:approval_required" json:"events"`

	// Enabled is the master switch（静默 = Enabled=false，保留配置不删除）.
	Enabled bool `gorm:"not null;default:true" json:"enabled"`

	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

func (Subscription) TableName() string { return "notification_subscriptions" }

// Wants reports whether the subscription covers the given event type.
func (s Subscription) Wants(evt string) bool {
	if s.Events == "" {
		return evt == "approval_required"
	}
	for _, e := range strings.Split(s.Events, ",") {
		if strings.TrimSpace(e) == evt {
			return true
		}
	}
	return false
}
