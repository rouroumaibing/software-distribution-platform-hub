package models

import (
	"time"

	"github.com/google/uuid"
)

// PersonalAccessToken is a user-owned API token (ui-12 §6.4 / S-1 裁定).
//
// S-1 裁定落地：hub 只存 **TokenHash**（sha256 hex），明文只在创建响应里出现
// 一次；Prefix 供列表掩码展示（sdp_pat_xxxx…）；Scopes 本期只支持 read/write
// 双档（写 = 全部非 GET）；ExpiresAt 可空 = 永不过期。鉴权链路：auth 中间件
// 识别 `sdp_pat_` 前缀 → 哈希查表 → 以属主身份注入 claims。
type PersonalAccessToken struct {
	ID uuid.UUID `gorm:"type:uuid;primaryKey;default:gen_random_uuid()" json:"id"`

	Subject string `gorm:"size:256;not null;index" json:"subject"`
	Name    string `gorm:"size:128;not null" json:"name"`

	Prefix    string `gorm:"size:16;not null" json:"prefix"`
	TokenHash string `gorm:"size:64;not null;uniqueIndex" json:"-"`
	Scopes    string `gorm:"size:32;not null;default:write" json:"scopes"` // read | write

	ExpiresAt  *time.Time `json:"expiresAt,omitempty"`
	RevokedAt  *time.Time `json:"revokedAt,omitempty"`
	LastUsedAt *time.Time `json:"lastUsedAt,omitempty"`
	CreatedAt  time.Time  `json:"createdAt"`
}

func (PersonalAccessToken) TableName() string { return "personal_access_tokens" }

// Scope constants for PersonalAccessToken.Scopes.
const (
	PATScopeRead  = "read"
	PATScopeWrite = "write"
)
