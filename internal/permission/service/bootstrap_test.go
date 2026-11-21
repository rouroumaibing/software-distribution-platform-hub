package service

import (
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/rouroumaibing/software-distribution-platform-hub/internal/permission/models"
)

// Legacy fixed IDs that the seed must not emit. Generated UUIDs are expected.
const (
	legacyFixedRoleID    = "f1000000-0000-0000-0000-000000000001"
	legacyFixedBindingID = "f3000000-0000-0000-0000-000000000001"
)

func bindingNaturalKey(subjectType, subjectID string, roleID uuid.UUID) string {
	return subjectType + "|" + subjectID + "|" + roleID.String()
}

// bootstrapRoleStub implements bootstrapRoleStore. Create fills a missing ID,
// mirroring the database's gen_random_uuid() default.
type bootstrapRoleStub struct {
	byName      map[string]*models.PlatformRole
	createErr   error
	createCalls int
}

func newBootstrapRoleStub() *bootstrapRoleStub {
	return &bootstrapRoleStub{byName: map[string]*models.PlatformRole{}}
}

func (s *bootstrapRoleStub) GetByName(name string) (*models.PlatformRole, error) {
	if r, ok := s.byName[name]; ok {
		return r, nil
	}
	return nil, errStubMissing
}

func (s *bootstrapRoleStub) Create(item *models.PlatformRole) error {
	s.createCalls++
	if s.createErr != nil {
		return s.createErr
	}
	if item.ID == uuid.Nil {
		item.ID = uuid.New()
	}
	s.byName[item.Name] = item
	return nil
}

// bootstrapBindingStub implements bootstrapBindingStore. It dedups on the
// same natural key as the repository's ExistsActive and fills a missing ID.
type bootstrapBindingStub struct {
	byKey       map[string]*models.PlatformRoleBinding
	createErr   error
	createCalls int
}

func newBootstrapBindingStub() *bootstrapBindingStub {
	return &bootstrapBindingStub{byKey: map[string]*models.PlatformRoleBinding{}}
}

func (s *bootstrapBindingStub) ExistsActive(_ *uuid.UUID, subjectType, subjectID string, roleID uuid.UUID) (bool, error) {
	_, ok := s.byKey[bindingNaturalKey(subjectType, subjectID, roleID)]
	return ok, nil
}

func (s *bootstrapBindingStub) Create(item *models.PlatformRoleBinding) error {
	s.createCalls++
	if s.createErr != nil {
		return s.createErr
	}
	if item.ID == uuid.Nil {
		item.ID = uuid.New()
	}
	s.byKey[bindingNaturalKey(item.SubjectType, item.SubjectID, item.PlatformRoleID)] = item
	return nil
}

func TestEnsureBuiltinPlatformRBAC_SeedsRoleAndBinding(t *testing.T) {
	roles := newBootstrapRoleStub()
	bindings := newBootstrapBindingStub()

	if err := EnsureBuiltinPlatformRBAC(roles, bindings); err != nil {
		t.Fatalf("EnsureBuiltinPlatformRBAC: %v", err)
	}
	role := roles.byName[BuiltinPlatformAdminRoleName]
	if role == nil {
		t.Fatal("built-in platform-admin role was not created")
	}
	if !role.IsSystem {
		t.Fatal("built-in role must be flagged IsSystem")
	}
	if role.ID == uuid.Nil || role.ID.String() == legacyFixedRoleID {
		t.Fatalf("role id = %s, want a generated UUID", role.ID)
	}
	b := bindings.byKey[bindingNaturalKey(SubjectTypeGroup, builtinAdminGroup, role.ID)]
	if b == nil {
		t.Fatal("bootstrap admin group binding was not created")
	}
	if b.ID == uuid.Nil || b.ID.String() == legacyFixedBindingID {
		t.Fatalf("binding id = %s, want a generated UUID", b.ID)
	}
	if b.SubjectType != SubjectTypeGroup || b.SubjectID != builtinAdminGroup {
		t.Fatalf("binding subject = %s/%s, want group/%s", b.SubjectType, b.SubjectID, builtinAdminGroup)
	}
	if b.PlatformRoleID != role.ID {
		t.Fatal("binding does not point at the built-in role")
	}
}

func TestEnsureBuiltinPlatformRBAC_IsIdempotent(t *testing.T) {
	roles := newBootstrapRoleStub()
	bindings := newBootstrapBindingStub()

	for i := 0; i < 3; i++ {
		if err := EnsureBuiltinPlatformRBAC(roles, bindings); err != nil {
			t.Fatalf("run %d: %v", i, err)
		}
	}
	if roles.createCalls != 1 {
		t.Fatalf("role create called %d times, want 1", roles.createCalls)
	}
	if bindings.createCalls != 1 {
		t.Fatalf("binding create called %d times, want 1", bindings.createCalls)
	}
}

func TestEnsureBuiltinPlatformRBAC_DoesNotOverwriteExisting(t *testing.T) {
	roles := newBootstrapRoleStub()
	bindings := newBootstrapBindingStub()
	custom := &models.PlatformRole{ID: uuid.New(), Name: BuiltinPlatformAdminRoleName, Description: "custom"}
	roles.byName[BuiltinPlatformAdminRoleName] = custom

	if err := EnsureBuiltinPlatformRBAC(roles, bindings); err != nil {
		t.Fatalf("EnsureBuiltinPlatformRBAC: %v", err)
	}
	if roles.createCalls != 0 {
		t.Fatal("existing role must not be recreated")
	}
	b := bindings.byKey[bindingNaturalKey(SubjectTypeGroup, builtinAdminGroup, custom.ID)]
	if b == nil || b.PlatformRoleID != custom.ID {
		t.Fatal("binding must point at the pre-existing role")
	}
}

func TestEnsureBuiltinPlatformRBAC_ReadWriteRaceRecovers(t *testing.T) {
	// Another replica inserts the role between our read and write: the name
	// read misses once, Create fails, and the follow-up read finds the row.
	// The binding is then deduped against that surviving role's ID.
	racedRole := &models.PlatformRole{ID: uuid.New(), Name: BuiltinPlatformAdminRoleName}
	raceRoles := &raceRoleStub{role: racedRole}
	raceBindings := &raceBindingStub{exists: true}

	if err := EnsureBuiltinPlatformRBAC(raceRoles, raceBindings); err != nil {
		t.Fatalf("race recovery: %v", err)
	}
	if raceBindings.checkedRoleID != racedRole.ID {
		t.Fatal("binding race check must use the surviving role's ID")
	}
}

// raceRoleStub reports a miss on the first name lookup, fails the create,
// then reports the row another replica inserted.
type raceRoleStub struct {
	calls int
	role  *models.PlatformRole
}

func (s *raceRoleStub) GetByName(string) (*models.PlatformRole, error) {
	s.calls++
	if s.calls == 1 {
		return nil, errStubMissing
	}
	return s.role, nil
}
func (s *raceRoleStub) Create(*models.PlatformRole) error { return errStubMissing }

// raceBindingStub reports no active grant, fails the create, then reports the
// grant another replica inserted on the follow-up check.
type raceBindingStub struct {
	calls         int
	exists        bool
	checkedRoleID uuid.UUID
}

func (s *raceBindingStub) ExistsActive(_ *uuid.UUID, _, _ string, roleID uuid.UUID) (bool, error) {
	s.calls++
	s.checkedRoleID = roleID
	if s.calls == 1 {
		return false, nil
	}
	return s.exists, nil
}
func (s *raceBindingStub) Create(*models.PlatformRoleBinding) error { return errStubMissing }

// componentRoleSeedStub implements componentRoleSeedStore. Create fills a
// missing ID, mirroring the database's gen_random_uuid() default.
type componentRoleSeedStub struct {
	byName      map[string]*models.ComponentRole
	createCalls int
}

func newComponentRoleSeedStub() *componentRoleSeedStub {
	return &componentRoleSeedStub{byName: map[string]*models.ComponentRole{}}
}

func (s *componentRoleSeedStub) GetByName(name string) (*models.ComponentRole, error) {
	if r, ok := s.byName[name]; ok {
		return r, nil
	}
	return nil, errStubMissing
}

func (s *componentRoleSeedStub) Create(item *models.ComponentRole) error {
	s.createCalls++
	if item.ID == uuid.Nil {
		item.ID = uuid.New()
	}
	s.byName[item.Name] = item
	return nil
}

func TestEnsureBuiltinComponentRoles_SeedsFourAndIsIdempotent(t *testing.T) {
	store := newComponentRoleSeedStub()
	if err := EnsureBuiltinComponentRoles(store); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if store.createCalls != 4 {
		t.Fatalf("expected 4 built-in roles seeded, got %d", store.createCalls)
	}
	for _, name := range []string{
		BuiltinComponentViewerRoleName, BuiltinComponentEditorRoleName,
		BuiltinComponentApproverRoleName, BuiltinComponentAdminRoleName,
	} {
		r, ok := store.byName[name]
		if !ok {
			t.Fatalf("missing built-in role %s", name)
		}
		if !r.IsSystem {
			t.Fatalf("built-in role %s must be system", name)
		}
		if len(r.Actions) == 0 || string(r.Actions) == "[]" {
			t.Fatalf("built-in role %s has empty actions", name)
		}
	}
	// Idempotence: a second pass must not create or overwrite existing roles.
	adminBefore := store.byName[BuiltinComponentAdminRoleName]
	adminBefore.Description = "locally edited"
	if err := EnsureBuiltinComponentRoles(store); err != nil {
		t.Fatalf("second seed: %v", err)
	}
	if store.createCalls != 4 {
		t.Fatalf("idempotence: expected no extra creates, got %d total", store.createCalls)
	}
	if store.byName[BuiltinComponentAdminRoleName].Description != "locally edited" {
		t.Fatal("idempotence: existing role was overwritten")
	}
}

func TestEnsureBuiltinComponentRoles_AdminHasTriggerAndRead(t *testing.T) {
	store := newComponentRoleSeedStub()
	if err := EnsureBuiltinComponentRoles(store); err != nil {
		t.Fatalf("seed: %v", err)
	}
	admin := string(store.byName[BuiltinComponentAdminRoleName].Actions)
	for _, action := range []string{"component:read", "pipeline:read", "pipeline:trigger", "artifact:read", "approval:approve"} {
		if !strings.Contains(admin, `"`+action+`"`) {
			t.Fatalf("component-admin missing action %s: %s", action, admin)
		}
	}
}
