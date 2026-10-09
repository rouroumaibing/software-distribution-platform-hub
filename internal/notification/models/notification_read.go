package models

import (
	"time"

	"gorm.io/gorm"
)

// NotificationRead is the server-side read cursor of the notification center
// (RUNNER-REFLUX-SPEC §6 / STATUS #20 hub 半边): one row per subject (token
// sub) with the timestamp up to which the subject has seen the notification
// stream. The hub computes unreadCount from it, eliminating the frontend
// clock skew the localStorage fallback (console 半边, 已上线) suffers from.
//
// Semantics: last_read_at only ever advances (read-mark with an older
// timestamp is a no-op) so a stale client can never re-mark seen items as
// unread.
type NotificationRead struct {
	// Subject is the authenticated principal (Keycloak sub / dev user).
	Subject string `gorm:"size:256;primaryKey" json:"subject"`

	LastReadAt time.Time `json:"lastReadAt"`
	CreatedAt  time.Time `json:"createdAt"`
	UpdatedAt  time.Time `json:"updatedAt"`

	DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`
}

func (NotificationRead) TableName() string { return "notification_reads" }

// ReadMark is the request body of POST /notifications/read-mark. ReadAt is
// the "seen up to" timestamp (only-forward semantics enforced by the
// service); empty defaults to server-now — which matches the console's
// "打开面板即推进游标" behavior.
type ReadMark struct {
	ReadAt *time.Time `json:"readAt,omitempty"`
}
