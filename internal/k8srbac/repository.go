package k8srbac

import (
	"github.com/google/uuid"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

// ClusterBinding is the hub-side record of a "集群权限" grant: a subject
// (user/group/org) bound to a predefined/custom role on a target cluster
// (environment). The k8s-side artifact is a ClusterRole/Role + corresponding
// ClusterRoleBinding/RoleBinding applied to that environment's cluster (§7).
// User subjects additionally get an issued kubeconfig (§8); its discriminator
// hash is persisted so revocation (§9) only swaps the cert subject.
type ClusterBinding struct {
	ID            uuid.UUID      `gorm:"type:uuid;primaryKey" json:"id"`
	EnvironmentID uuid.UUID      `gorm:"type:uuid;index" json:"environmentId"`
	SubjectKind   string         `gorm:"size:16;not null" json:"subjectKind"` // user|group|org
	SubjectID     string         `gorm:"size:64;index" json:"subjectId"`     // hub-side ID
	SubjectName   string         `gorm:"size:255" json:"subjectName"`        // KC group path / username (for Group/Org subject name)
	Role          string         `gorm:"size:32;not null" json:"role"`
	ClusterScoped bool           `json:"clusterScoped"` // true→ClusterRole+ClusterRoleBinding; false→Role+RoleBinding
	Namespace     string         `gorm:"size:64" json:"namespace"`
	Resources     datatypes.JSON `json:"resources"` // custom role only
	Operations    datatypes.JSON `json:"operations"` // custom role only
	CertHash      string         `gorm:"size:64" json:"certHash"` // User subject only
	Status        string         `gorm:"size:16;not null" json:"status"` // active|revoked
}

func (ClusterBinding) TableName() string { return "cluster_bindings" }

// Status values.
const (
	StatusActive  = "active"
	StatusRevoked = "revoked"
)

// Repository persists ClusterBinding rows.
type Repository struct{ DB *gorm.DB }

func NewRepository(db *gorm.DB) *Repository { return &Repository{DB: db} }

func (r *Repository) Create(b *ClusterBinding) error { return r.DB.Create(b).Error }

func (r *Repository) Get(id uuid.UUID) (*ClusterBinding, error) {
	var b ClusterBinding
	if err := r.DB.First(&b, "id = ?", id).Error; err != nil {
		return nil, err
	}
	return &b, nil
}

func (r *Repository) ListByEnvironment(envID uuid.UUID) ([]ClusterBinding, error) {
	var bs []ClusterBinding
	if err := r.DB.Where("environment_id = ?", envID).Find(&bs).Error; err != nil {
		return nil, err
	}
	return bs, nil
}

func (r *Repository) ListBySubject(subjectKind, subjectID string) ([]ClusterBinding, error) {
	var bs []ClusterBinding
	if err := r.DB.Where("subject_kind = ? AND subject_id = ?", subjectKind, subjectID).Find(&bs).Error; err != nil {
		return nil, err
	}
	return bs, nil
}

func (r *Repository) Update(b *ClusterBinding) error { return r.DB.Save(b).Error }

func (r *Repository) SetStatus(id uuid.UUID, status string) error {
	return r.DB.Model(&ClusterBinding{}).Where("id = ?", id).Update("status", status).Error
}

func (r *Repository) Delete(id uuid.UUID) error {
	return r.DB.Delete(&ClusterBinding{}, "id = ?", id).Error
}
