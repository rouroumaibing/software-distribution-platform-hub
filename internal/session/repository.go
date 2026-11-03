package session

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"gorm.io/gorm"

	"github.com/rouroumaibing/software-distribution-platform-hub/internal/credentials/codec"
)

// ErrNotFound is returned by Get when no unexpired session matches the id.
var ErrNotFound = errors.New("session: not found")

type Repository struct {
	db *gorm.DB
}

func NewRepository(db *gorm.DB) *Repository { return &Repository{db: db} }

// Create inserts a new session row. KCToken is encrypted at rest; the caller
// passes the plaintext access token.
func (r *Repository) Create(ctx context.Context, s *Session) error {
	if s.KCToken != "" {
		enc, err := codec.Encrypt(s.KCToken)
		if err != nil {
			return err
		}
		s.KCToken = enc
	}
	return r.db.WithContext(ctx).Create(s).Error
}

// Get returns the unexpired session for id. KCToken is decrypted to plaintext.
func (r *Repository) Get(ctx context.Context, id string) (*Session, error) {
	var s Session
	if err := r.db.WithContext(ctx).Where("id = ? AND expires_at > ?", id, time.Now()).First(&s).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if s.KCToken != "" {
		pt, err := codec.Decrypt(s.KCToken)
		if err != nil {
			return nil, err
		}
		s.KCToken = pt
	}
	return &s, nil
}

// GroupsList decodes the JSON-encoded groups column.
func (s *Session) GroupsList() []string {
	if s.Groups == "" {
		return nil
	}
	var g []string
	_ = json.Unmarshal([]byte(s.Groups), &g)
	return g
}

func (r *Repository) Delete(ctx context.Context, id string) error {
	return r.db.WithContext(ctx).Where("id = ?", id).Delete(&Session{}).Error
}

// DeleteBySubject revokes every session of a subject (used after password
// change so stale sessions cannot be reused).
func (r *Repository) DeleteBySubject(ctx context.Context, subject string) error {
	return r.db.WithContext(ctx).Where("subject = ?", subject).Delete(&Session{}).Error
}
