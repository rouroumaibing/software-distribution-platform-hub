package service

import (
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/rouroumaibing/software-distribution-platform-hub/internal/common"
	"github.com/rouroumaibing/software-distribution-platform-hub/internal/permission/models"
)

// Subject types accepted by every binding table (§5.3).
const (
	SubjectTypeUser  = "user"
	SubjectTypeGroup = "group"
)

// OrgGroupPrefix is the reserved namespace for organization groups
// (ACCOUNT-PERMISSION-MODEL §2.3). Groups under this prefix identify the org
// dimension; they must never be used as an RBAC subject, so the binding API
// refuses them. Keeping org identity out of subject_id is what lets the org
// carrier be swapped without migrating any stored binding.
const OrgGroupPrefix = "/org:"

// PlatformBindingStore is the narrow persistence surface the service needs.
type PlatformBindingStore interface {
	List(orgID *uuid.UUID, subjectType, subjectID string) ([]models.PlatformRoleBinding, error)
	GetByID(id uuid.UUID) (*models.PlatformRoleBinding, error)
	Create(item *models.PlatformRoleBinding) error
	Delete(id uuid.UUID) error
	ExistsActive(orgID *uuid.UUID, subjectType, subjectID string, roleID uuid.UUID) (bool, error)
}

// PlatformRoleLookup resolves the role a binding points at. Split out from
// PlatformBindingStore so a binding test can stub role resolution without
// also faking the binding table.
type PlatformRoleLookup interface {
	GetByID(id uuid.UUID) (*models.PlatformRole, error)
}

// PlatformBindingService grants platform-level roles to subjects (§5.1 ②).
type PlatformBindingService struct {
	store    PlatformBindingStore
	roleRepo PlatformRoleLookup
	now      func() time.Time
}

func NewPlatformBindingService(store PlatformBindingStore, roleRepo PlatformRoleLookup) *PlatformBindingService {
	return &PlatformBindingService{store: store, roleRepo: roleRepo, now: time.Now}
}

func errSubjectTypeInvalid() *common.APIError {
	return common.DomainError(common.KindPermission, http.StatusBadRequest, 20, "subjectType must be user or group")
}

func errSubjectIDRequired() *common.APIError {
	return common.DomainError(common.KindPermission, http.StatusBadRequest, 21, "subjectId is required")
}

func errGroupSubjectNeedsSlash() *common.APIError {
	return common.DomainError(common.KindPermission, http.StatusBadRequest, 22,
		"group subjectId must match the token claim verbatim — with full.path=true every group starts with '/'")
}

func errOrgGroupNotASubject() *common.APIError {
	return common.DomainError(common.KindPermission, http.StatusBadRequest, 23,
		"groups under the reserved '/org:' namespace identify an organization and cannot be an RBAC subject")
}

func errPlatformRoleIDRequired() *common.APIError {
	return common.DomainError(common.KindPermission, http.StatusBadRequest, 24, "platformRoleId is required")
}

func errExpiryNotFuture() *common.APIError {
	return common.DomainError(common.KindPermission, http.StatusBadRequest, 25, "expiresAt must be in the future")
}

func errPlatformBindingExists() *common.APIError {
	return common.DomainError(common.KindPermission, http.StatusConflict, 5, "Platform Role Binding Already Exists")
}

func errPlatformBindingNotFound() *common.APIError {
	return common.DomainError(common.KindPermission, http.StatusNotFound, 2, "Platform Role Binding Not Found")
}

// NormalizeSubject validates a (subjectType, subjectId) pair against the
// §5.3 contract and returns the trimmed values.
//
//	user  → the Keycloak `sub`. Shape alone cannot prove it IS a sub, so the
//	        service only enforces non-emptiness here; the console is
//	        responsible for sourcing the value from the token, never from its
//	        local users table (which is being deleted, §12 D3).
//	group → the token claim verbatim. This realm mounts the groups mapper with
//	        full.path=true, so every group arrives as "/name" with a leading
//	        slash; a binding written as "sdp-admins" would silently never
//	        match. The check turns that silent failure into a 400.
func NormalizeSubject(subjectType, subjectID string) (string, string, error) {
	st := strings.TrimSpace(subjectType)
	id := strings.TrimSpace(subjectID)
	switch st {
	case SubjectTypeUser:
		if id == "" {
			return "", "", errSubjectIDRequired()
		}
	case SubjectTypeGroup:
		if id == "" {
			return "", "", errSubjectIDRequired()
		}
		if !strings.HasPrefix(id, "/") {
			return "", "", errGroupSubjectNeedsSlash()
		}
		if strings.HasPrefix(id, OrgGroupPrefix) {
			return "", "", errOrgGroupNotASubject()
		}
	default:
		return "", "", errSubjectTypeInvalid()
	}
	return st, id, nil
}

// List returns platform bindings, optionally narrowed. Expired grants are
// included on purpose: the management view has to show what lapsed.
func (s *PlatformBindingService) List(orgID *uuid.UUID, subjectType, subjectID string) ([]models.PlatformRoleBinding, error) {
	st := strings.TrimSpace(subjectType)
	if st != "" && st != SubjectTypeUser && st != SubjectTypeGroup {
		// A typo in the filter must not read as "no bindings exist".
		return nil, errSubjectTypeInvalid()
	}
	return s.store.List(orgID, st, strings.TrimSpace(subjectID))
}

// Create validates and inserts a platform binding.
func (s *PlatformBindingService) Create(in *models.PlatformRoleBinding) error {
	st, id, err := NormalizeSubject(in.SubjectType, in.SubjectID)
	if err != nil {
		return err
	}
	in.SubjectType, in.SubjectID = st, id

	if in.PlatformRoleID == uuid.Nil {
		return errPlatformRoleIDRequired()
	}
	if _, rerr := s.roleRepo.GetByID(in.PlatformRoleID); rerr != nil {
		return errPlatformRoleNotFound()
	}
	if in.ExpiresAt != nil && !in.ExpiresAt.After(s.now()) {
		// A grant that is born expired is a bug at the call site, not a
		// no-op: reject it instead of storing a row nothing will ever honour.
		return errExpiryNotFuture()
	}
	exists, eerr := s.store.ExistsActive(in.OrgID, in.SubjectType, in.SubjectID, in.PlatformRoleID)
	if eerr != nil {
		return eerr
	}
	if exists {
		return errPlatformBindingExists()
	}
	return s.store.Create(in)
}

func (s *PlatformBindingService) Delete(id uuid.UUID) error {
	if _, err := s.store.GetByID(id); err != nil {
		return errPlatformBindingNotFound()
	}
	return s.store.Delete(id)
}
