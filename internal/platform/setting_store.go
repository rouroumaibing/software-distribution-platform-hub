package platform

import (
	"errors"

	"gorm.io/gorm"

	models "github.com/rouroumaibing/software-distribution-platform-hub/internal/platform/models"
)

// SettingStore reads/writes platform_settings rows. Tests inject fakes.
type SettingStore interface {
	Get(key string) (string, error) // gorm.ErrRecordNotFound = 未设置
	Put(key, value string) error
}

// GormSettingStore is the Postgres-backed SettingStore.
type GormSettingStore struct{ db *gorm.DB }

func NewGormSettingStore(db *gorm.DB) *GormSettingStore { return &GormSettingStore{db: db} }

func (s *GormSettingStore) Get(key string) (string, error) {
	var row models.Setting
	if err := s.db.Where("key = ?", key).First(&row).Error; err != nil {
		return "", err
	}
	return row.Value, nil
}

func (s *GormSettingStore) Put(key, value string) error {
	row := models.Setting{Key: key, Value: value}
	return s.db.Save(&row).Error
}

// LoadChangeWindow reads the change-window setting. 未设置/损坏 JSON → Enabled=false
// 的默认档（fail-open：管控未显式开启就放行——这是特性开关语义，与生产强审批
// 的 fail-closed 相反：变更窗口挡的是"方便"，不是"不可逆事故"）。
func LoadChangeWindow(store SettingStore) (ChangeWindow, error) {
	raw, err := store.Get(models.SettingKeyChangeWindow)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return ChangeWindow{}, nil
	}
	if err != nil {
		return ChangeWindow{}, err
	}
	return ParseChangeWindow(raw), nil
}

// SaveChangeWindow validates then stores the change-window setting.
func SaveChangeWindow(store SettingStore, cw ChangeWindow) error {
	v, err := EncodeChangeWindow(cw)
	if err != nil {
		return err
	}
	return store.Put(models.SettingKeyChangeWindow, v)
}
