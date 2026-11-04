package models

import (
	"encoding/json"

	"github.com/rouroumaibing/software-distribution-platform-hub/internal/common"
	"gorm.io/datatypes"
)

type Org struct {
	common.Base
	Name        string         `gorm:"size:128;not null" json:"name"`
	Slug        string         `gorm:"size:64;not null;uniqueIndex" json:"slug"`
	Annotations datatypes.JSON `json:"annotations,omitempty"`
	Quotas      datatypes.JSON `json:"quotas,omitempty"`
}

func (Org) TableName() string { return "orgs" }

// OrgAnnotations is the structured view of Org.Annotations. OrgManager is the
// single source of truth for L2 (org_admin) membership: when an org is created
// its manager's username is written here and mirrored into that user's Keycloak
// `org_admin` attribute (ACCESS-CONTROL §1.2 / §3).
type OrgAnnotations struct {
	OrgManager string `json:"org_manager"`
}

// DecodeAnnotations returns the structured annotations, tolerating an empty or
// invalid blob (returns a zero value rather than erroring).
func (o Org) DecodeAnnotations() OrgAnnotations {
	var a OrgAnnotations
	if len(o.Annotations) > 0 {
		_ = json.Unmarshal(o.Annotations, &a)
	}
	return a
}

// SetOrgManager writes the org manager into the annotations blob.
func (o *Org) SetOrgManager(username string) error {
	b, err := json.Marshal(OrgAnnotations{OrgManager: username})
	if err != nil {
		return err
	}
	o.Annotations = b
	return nil
}

// DefaultOrg 组织列表为空时返回的默认组织（模式对位 old
// pkg/apis/servicetree.DefaultServiceTreeTableString：默认数据定义在实体
// 契约层，List 读到空表时就地补插，保证首次使用页面不为空）。
func DefaultOrg() *Org {
	return &Org{Name: "Default", Slug: "default"}
}
