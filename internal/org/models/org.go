package models

import "github.com/rouroumaibing/software-distribution-platform-hub/internal/common"

type Org struct {
	common.Base
	Name string `gorm:"size:128;not null" json:"name"`
	Slug string `gorm:"size:64;not null;uniqueIndex" json:"slug"`
}

func (Org) TableName() string { return "orgs" }

// DefaultOrg 组织列表为空时返回的默认组织（模式对位 old
// pkg/apis/servicetree.DefaultServiceTreeTableString：默认数据定义在实体
// 契约层，List 读到空表时就地补插，保证首次使用页面不为空）。
func DefaultOrg() *Org {
	return &Org{Name: "Default", Slug: "default"}
}
