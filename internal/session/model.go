// Package session owns the hub-side login session (AUTH-ARCHITECTURE.md §4).
//
// The browser holds only an AES-GCM-encrypted session id in an httpOnly cookie;
// the row carries subject / username / groups / the Keycloak access token
// (encrypted at rest via the credentials codec, for userinfo refresh and
// logout propagation) / expiry. The DB is the source of truth — the cookie is
// merely a lookup key, so revoking a session is a single DELETE.
package session

import "time"

// Session is a hub login session row. GORM tags follow the hub schema SSOT
// (AutoMigrate, only-add). Table name "sessions".
type Session struct {
	ID        string    `gorm:"primaryKey;type:uuid;default:gen_random_uuid()" json:"-"`
	Subject   string    `gorm:"index;not null" json:"subject"`
	Username  string    `json:"username,omitempty"`
	Groups    string    `gorm:"type:text" json:"-"` // JSON-encoded []string
	KCToken   string    `gorm:"type:text" json:"-"` // codec.Encrypt'd access_token (plaintext in memory after Get)
	ExpiresAt time.Time `gorm:"index" json:"expiresAt"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

func (Session) TableName() string { return "sessions" }
