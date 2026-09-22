package service

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/rouroumaibing/software-distribution-platform-hub/internal/common"
	"github.com/rouroumaibing/software-distribution-platform-hub/internal/permission/models"
)

// stubComponentRoleStore keys rows by id and by the (org, name) pair the
// service actually queries on — so the "duplicate in this org" / "same name in
// *another* org" cases are both expressible.
type stubComponentRoleStore struct {
	byID      map[uuid.UUID]*models.ComponentRole
	byOrgName map[string]*models.ComponentRole
	created   []*models.ComponentRole
	updated   []*models.ComponentRole
	deleted   []uuid.UUID
	bindings  int64
}

func newStubComponentRoleStore() *stubComponentRoleStore {
	return &stubComponentRoleStore{
		byID:      map[uuid.UUID]*models.ComponentRole{},
		byOrgName: map[string]*models.ComponentRole{},
	}
}

// orgNameKey mirrors what GetByNameInOrg matches: built-ins (nil org) live in
// their own namespace rather than sharing the empty-string one by accident.
func orgNameKey(orgID *uuid.UUID, name string) string {
	if orgID == nil {
		return "<builtin>/" + name
	}
	return orgID.String() + "/" + name
}

func (s *stubComponentRoleStore) List() ([]models.ComponentRole, error) {
	out := make([]models.ComponentRole, 0, len(s.byID))
	for _, r := range s.byID {
		out = append(out, *r)
	}
	return out, nil
}

func (s *stubComponentRoleStore) GetByID(id uuid.UUID) (*models.ComponentRole, error) {
	if r, ok := s.byID[id]; ok {
		return r, nil
	}
	return nil, errStubMissing
}

func (s *stubComponentRoleStore) GetByNameInOrg(orgID *uuid.UUID, name string) (*models.ComponentRole, error) {
	if r, ok := s.byOrgName[orgNameKey(orgID, name)]; ok {
		return r, nil
	}
	return nil, errStubMissing
}

func (s *stubComponentRoleStore) Create(item *models.ComponentRole) error {
	s.created = append(s.created, item)
	return nil
}

func (s *stubComponentRoleStore) Update(item *models.ComponentRole) error {
	s.updated = append(s.updated, item)
	return nil
}

func (s *stubComponentRoleStore) Delete(id uuid.UUID) error {
	s.deleted = append(s.deleted, id)
	return nil
}

func (s *stubComponentRoleStore) CountBindings(uuid.UUID) (int64, error) { return s.bindings, nil }

func TestComponentRoleCreate_Validation(t *testing.T) {
	cases := []struct {
		name     string
		roleName string
		actions  []byte
		wantCode string
	}{
		{"blank name", "   ", validActionsJSON, "ERR.10400020"},
		{"empty actions array", "custom", []byte("[]"), "ERR.10400021"},
		{"actions not an array", "custom", []byte(`{"a":1}`), "ERR.10400021"},
		{"actions only blank strings", "custom", []byte(`["  "]`), "ERR.10400021"},
		{"actions absent", "custom", nil, "ERR.10400021"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store := newStubComponentRoleStore()
			svc := NewComponentRoleService(store)

			err := svc.Create(&models.ComponentRole{Name: tc.roleName, Actions: tc.actions})
			if got := errCode(t, err); got != tc.wantCode {
				t.Fatalf("错误码 = %s，期望 %s", got, tc.wantCode)
			}
			if len(store.created) != 0 {
				t.Fatalf("校验失败却已写库")
			}
		})
	}
}

func TestComponentRoleCreate_RejectsDuplicateNameInSameOrg(t *testing.T) {
	org := uuid.New()
	store := newStubComponentRoleStore()
	existing := &models.ComponentRole{ID: uuid.New(), OrgID: &org, Name: "release-owner"}
	store.byOrgName[orgNameKey(&org, "release-owner")] = existing
	store.byID[existing.ID] = existing
	svc := NewComponentRoleService(store)

	err := svc.Create(&models.ComponentRole{OrgID: &org, Name: "release-owner", Actions: validActionsJSON})
	if got := errCode(t, err); got != "ERR.10409022" {
		t.Fatalf("错误码 = %s，期望 ERR.10409022", got)
	}
	if len(store.created) != 0 {
		t.Fatalf("同组织重名却已写库")
	}
}

// 唯一键是 (org_id, name) 而不是全局 name：两个组织各自定义同名角色是合理的，
// 按全局唯一会让他们互相踩。这条断言把该语义钉死。
func TestComponentRoleCreate_AllowsSameNameInDifferentOrg(t *testing.T) {
	orgA, orgB := uuid.New(), uuid.New()
	store := newStubComponentRoleStore()
	existing := &models.ComponentRole{ID: uuid.New(), OrgID: &orgA, Name: "release-owner"}
	store.byOrgName[orgNameKey(&orgA, "release-owner")] = existing
	store.byID[existing.ID] = existing
	svc := NewComponentRoleService(store)

	in := &models.ComponentRole{OrgID: &orgB, Name: "release-owner", Actions: validActionsJSON}
	if err := svc.Create(in); err != nil {
		t.Fatalf("另一组织同名应通过，实际 %v", err)
	}
	if len(store.created) != 1 {
		t.Fatalf("应写入 1 行，实际 %d", len(store.created))
	}
}

// 内置角色（org_id is null）同名同样不能重复：GetByName 按名字解析内置角色
// （owner 引导 / 绑定校验都走它），两条同名内置角色会让解析结果二义。
func TestComponentRoleCreate_RejectsDuplicateBuiltInName(t *testing.T) {
	store := newStubComponentRoleStore()
	builtin := &models.ComponentRole{ID: uuid.New(), Name: "component-admin", IsSystem: true}
	store.byOrgName[orgNameKey(nil, "component-admin")] = builtin
	store.byID[builtin.ID] = builtin
	svc := NewComponentRoleService(store)

	err := svc.Create(&models.ComponentRole{Name: "component-admin", Actions: validActionsJSON})
	if got := errCode(t, err); got != "ERR.10409022" {
		t.Fatalf("错误码 = %s，期望 ERR.10409022", got)
	}
}

func TestComponentRoleCreate_CannotForgeBuiltIn(t *testing.T) {
	store := newStubComponentRoleStore()
	svc := NewComponentRoleService(store)

	in := &models.ComponentRole{Name: "sneaky", Actions: validActionsJSON, IsSystem: true}
	if err := svc.Create(in); err != nil {
		t.Fatalf("应通过，实际 %v", err)
	}
	if in.IsSystem {
		t.Fatalf("IsSystem 必须由服务端置 false，实际 true")
	}
}

func TestComponentRoleUpdate(t *testing.T) {
	id, org := uuid.New(), uuid.New()
	created := time.Now().Add(-time.Hour)
	newCustom := func() *stubComponentRoleStore {
		store := newStubComponentRoleStore()
		r := &models.ComponentRole{ID: id, OrgID: &org, Name: "old", Actions: validActionsJSON, CreatedAt: created}
		store.byID[id] = r
		store.byOrgName[orgNameKey(&org, "old")] = r
		return store
	}

	t.Run("unknown id maps to 404", func(t *testing.T) {
		svc := NewComponentRoleService(newStubComponentRoleStore())
		err := svc.Update(uuid.New(), &models.ComponentRole{Name: "x", Actions: validActionsJSON})
		if got := errCode(t, err); got != "ERR.10404023" {
			t.Fatalf("错误码 = %s，期望 ERR.10404023", got)
		}
	})

	// 内置角色可改的话，一次误操作就能把 component-admin 的 actions 清空，
	// 组件 owner 的引导授权随之消失。
	t.Run("built-in is immutable", func(t *testing.T) {
		store := newStubComponentRoleStore()
		builtin := &models.ComponentRole{ID: id, Name: "component-admin", IsSystem: true, Actions: validActionsJSON}
		store.byID[id] = builtin
		svc := NewComponentRoleService(store)

		err := svc.Update(id, &models.ComponentRole{Name: "component-admin", Actions: validActionsJSON})
		if got := errCode(t, err); got != "ERR.10409024" {
			t.Fatalf("错误码 = %s，期望 ERR.10409024", got)
		}
		if len(store.updated) != 0 {
			t.Fatalf("内置角色被改写")
		}
	})

	t.Run("rename onto another role in the same org is a conflict", func(t *testing.T) {
		store := newCustom()
		other := &models.ComponentRole{ID: uuid.New(), OrgID: &org, Name: "taken"}
		store.byOrgName[orgNameKey(&org, "taken")] = other
		svc := NewComponentRoleService(store)

		err := svc.Update(id, &models.ComponentRole{Name: "taken", Actions: validActionsJSON})
		if got := errCode(t, err); got != "ERR.10409022" {
			t.Fatalf("错误码 = %s，期望 ERR.10409022", got)
		}
	})

	// 改名时不得把自己判成"重名"：查重命中自身必须放行，否则任何一次改名都会 409。
	t.Run("renaming to its own current name is allowed", func(t *testing.T) {
		store := newCustom()
		svc := NewComponentRoleService(store)

		if err := svc.Update(id, &models.ComponentRole{Name: "old", Description: "d", Actions: validActionsJSON}); err != nil {
			t.Fatalf("应通过，实际 %v", err)
		}
		if len(store.updated) != 1 {
			t.Fatalf("应写回 1 行，实际 %d", len(store.updated))
		}
	})

	// Read-modify-write：Save 写全部映射列，若不带回服务端拥有的字段，
	// org_id / is_system / created_at 会被写成零值 —— 角色会静默"漂"到别的组织。
	t.Run("happy path preserves server-owned fields", func(t *testing.T) {
		store := newCustom()
		svc := NewComponentRoleService(store)

		next := []byte(`["component:editor"]`)
		if err := svc.Update(id, &models.ComponentRole{Name: "new", Description: "d", Actions: next}); err != nil {
			t.Fatalf("应通过，实际 %v", err)
		}
		if len(store.updated) != 1 {
			t.Fatalf("应写回 1 行，实际 %d", len(store.updated))
		}
		got := store.updated[0]
		if got.ID != id || got.OrgID == nil || *got.OrgID != org || !got.CreatedAt.Equal(created) {
			t.Fatalf("服务端字段被覆盖：id=%v org=%v created=%v", got.ID, got.OrgID, got.CreatedAt)
		}
		if got.Name != "new" || got.Description != "d" || !bytes.Equal(got.Actions, next) {
			t.Fatalf("调用方字段未写入：name=%q desc=%q actions=%s", got.Name, got.Description, got.Actions)
		}
	})
}

// 组织归属不可通过 PUT 迁移：把角色挪到另一个组织，会同时改变"它授权谁"，
// 而调用方以为只是改了个名字/描述。请求体里的 orgId 必须被忽略。
func TestComponentRoleUpdate_OrgScopeIsNotUpdatable(t *testing.T) {
	id, orgA, orgB := uuid.New(), uuid.New(), uuid.New()
	store := newStubComponentRoleStore()
	r := &models.ComponentRole{ID: id, OrgID: &orgA, Name: "old", Actions: validActionsJSON}
	store.byID[id] = r
	store.byOrgName[orgNameKey(&orgA, "old")] = r
	svc := NewComponentRoleService(store)

	if err := svc.Update(id, &models.ComponentRole{OrgID: &orgB, Name: "new", Actions: validActionsJSON}); err != nil {
		t.Fatalf("应通过，实际 %v", err)
	}
	if got := store.updated[0].OrgID; got == nil || *got != orgA {
		t.Fatalf("org 归属被改写为 %v（请求体带入的 orgId 必须被忽略）", got)
	}
}

func TestComponentRoleDelete(t *testing.T) {
	id := uuid.New()

	t.Run("built-in is immutable", func(t *testing.T) {
		store := newStubComponentRoleStore()
		store.byID[id] = &models.ComponentRole{ID: id, Name: "component-admin", IsSystem: true}
		svc := NewComponentRoleService(store)

		if got := errCode(t, svc.Delete(id)); got != "ERR.10409024" {
			t.Fatalf("错误码 = %s，期望 ERR.10409024", got)
		}
		if len(store.deleted) != 0 {
			t.Fatalf("内置角色被删除")
		}
	})

	t.Run("in use is refused with reasons", func(t *testing.T) {
		store := newStubComponentRoleStore()
		store.byID[id] = &models.ComponentRole{ID: id, Name: "custom"}
		store.bindings = 3
		svc := NewComponentRoleService(store)

		err := svc.Delete(id)
		if got := errCode(t, err); got != "ERR.10409025" {
			t.Fatalf("错误码 = %s，期望 ERR.10409025", got)
		}
		ae := err.(*common.APIError)
		if len(ae.Reasons) == 0 || !strings.Contains(strings.Join(ae.Reasons, ""), "3") {
			t.Fatalf("reasons 应带引用条数，实际 %v", ae.Reasons)
		}
		if len(store.deleted) != 0 {
			t.Fatalf("仍被引用却已删除")
		}
	})

	t.Run("unknown id maps to 404", func(t *testing.T) {
		svc := NewComponentRoleService(newStubComponentRoleStore())
		if got := errCode(t, svc.Delete(uuid.New())); got != "ERR.10404023" {
			t.Fatalf("错误码 = %s，期望 ERR.10404023", got)
		}
	})

	t.Run("happy path deletes", func(t *testing.T) {
		store := newStubComponentRoleStore()
		store.byID[id] = &models.ComponentRole{ID: id, Name: "custom"}
		svc := NewComponentRoleService(store)

		if err := svc.Delete(id); err != nil {
			t.Fatalf("应通过，实际 %v", err)
		}
		if len(store.deleted) != 1 || store.deleted[0] != id {
			t.Fatalf("未执行删除：%v", store.deleted)
		}
	})
}

// 错误码家族不得与平台级角色（seq 1–4）撞车：两处都是 409，共用同一个
// ERR.1040900x 会让排障时无法区分"哪张表的角色冲突"。
func TestComponentRoleErrorCodesDoNotCollideWithPlatformRole(t *testing.T) {
	componentCodes := []string{
		errComponentRoleNameRequired().ErrorCode,
		errComponentRoleActionsRequired().ErrorCode,
		errComponentRoleExists().ErrorCode,
		errComponentRoleNotFound().ErrorCode,
		errComponentRoleImmutable().ErrorCode,
		errComponentRoleInUse(1).ErrorCode,
	}
	seen := map[string]bool{}
	for _, c := range componentCodes {
		if seen[c] {
			t.Fatalf("组件级角色错误码重复：%s", c)
		}
		seen[c] = true
	}
	platformCodes := map[string]bool{
		errPlatformRoleNameRequired().ErrorCode:    true,
		errPlatformRoleActionsRequired().ErrorCode: true,
		errPlatformRoleExists().ErrorCode:          true,
		errPlatformRoleNotFound().ErrorCode:        true,
		errPlatformRoleImmutable().ErrorCode:       true,
		errPlatformRoleInUse(1).ErrorCode:          true,
	}
	for c := range seen {
		if platformCodes[c] {
			t.Errorf("组件级与平台级角色共用错误码 %s", c)
		}
	}
}
