package notification

import (
	"errors"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/rouroumaibing/software-distribution-platform-hub/internal/notification/models"
)

// GormSubscriptionStore persists notification_subscriptions. Upsert keeps the
// row-level primary key on subject — one subscription per subject.
type GormSubscriptionStore struct{ db *gorm.DB }

func NewGormSubscriptionStore(db *gorm.DB) *GormSubscriptionStore {
	return &GormSubscriptionStore{db: db}
}

func (s *GormSubscriptionStore) Get(subject string) (models.Subscription, error) {
	var out models.Subscription
	err := s.db.Where("subject = ?", subject).First(&out).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		// 无订阅行 = 未订阅（Wants 恒 false 的默认行语义）
		return models.Subscription{Subject: subject}, nil
	}
	return out, err
}

func (s *GormSubscriptionStore) Upsert(sub *models.Subscription) error {
	return s.db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "subject"}},
		DoUpdates: clause.AssignmentColumns([]string{"email", "events", "enabled", "updated_at"}),
	}).Create(sub).Error
}
