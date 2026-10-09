package notification

import (
	"errors"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/rouroumaibing/software-distribution-platform-hub/internal/notification/models"
)

// errReadStoreUnavailable is returned by MarkRead when no read store is
// wired (old DB without notification_reads). The handler maps it to 503 so
// the console keeps its localStorage fallback instead of surfacing an error.
var errReadStoreUnavailable = errors.New("notification: read store unavailable")

// ReadStore persists the per-subject server-side read cursor
// (RUNNER-REFLUX-SPEC §6): notification_reads, one row per subject.
type ReadStore struct{ DB *gorm.DB }

func NewReadStore(db *gorm.DB) *ReadStore { return &ReadStore{DB: db} }

// LastReadAt returns the subject's cursor; zero time when the subject has
// never marked read (everything is unread).
func (r *ReadStore) LastReadAt(subject string) (time.Time, error) {
	var row models.NotificationRead
	if err := r.DB.Where("subject = ?", subject).First(&row).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return time.Time{}, nil
		}
		return time.Time{}, err
	}
	return row.LastReadAt, nil
}

// AdvanceTo moves the cursor forward to at (only-forward: an older timestamp
// is silently ignored, so a stale client can never resurrect read items).
func (r *ReadStore) AdvanceTo(subject string, at time.Time) error {
	row := models.NotificationRead{Subject: subject, LastReadAt: at}
	return r.DB.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "subject"}},
		DoUpdates: clause.Assignments(map[string]any{
			"last_read_at": gorm.Expr("GREATEST(notification_reads.last_read_at, EXCLUDED.last_read_at)"),
		}),
	}).Create(&row).Error
}
