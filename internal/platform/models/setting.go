// Package platform 承载平台级设置（ui-12 §6.3 平台设置 / §5.1 变更窗口）。
//
// hub 无用户表（D3），平台设置同样无状态表依赖：单表 KV（key → JSON value），
// 每个设置项一个 key，value 是该项的 JSON 编码。新设置项 = 新 key + 新解析
// 结构体，不建新表。
package platform

import "time"

// Setting is one platform-level setting row. Value holds the JSON-encoded
// payload for that key; unknown keys are preserved (只加不删的 AutoMigrate 口径).
type Setting struct {
	Key       string    `gorm:"size:64;primaryKey" json:"key"`
	Value     string    `gorm:"type:text" json:"value"`
	UpdatedAt time.Time `json:"updatedAt"`
}

func (Setting) TableName() string { return "platform_settings" }

// SettingKeyChangeWindow is the §5.1 变更窗口 key. Value = ChangeWindow JSON.
const SettingKeyChangeWindow = "change_window"
