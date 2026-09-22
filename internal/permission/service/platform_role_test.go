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

// stubRoleStore keeps roles addressable by id and by name so a test can set up
// the exact "already taken" / "is a built-in" states the service has to react to.
type stubRoleStore struct {
	byID     map[uuid.UUID]*models.PlatformRole
	byName   map[string]*models.PlatformRole
	created  []*models.PlatformRole
	updated  []*models.PlatformRole
	deleted  []uuid.UUID
	bindings int64
}

func newStubRoleStore() *stubRoleStore {
	return &stubRoleStore{byID: map[uuid.UUID]*models.PlatformRole{}, byName: map[string]*models.PlatformRole{}}
}

var errStubMissing = common.NewAPIError(common.KindBase, 404, 1, "missing")

func (s *stubRoleStore) List() ([]models.PlatformRole, error) {
	out := make([]models.PlatformRole, 0, len(s.byID))
	for _, r := range s.byID {
		out = append(out, *r)
	}
	return out, nil
}

func (s *stubRoleStore) GetByID(id uuid.UUID) (*models.PlatformRole, error) {
	if r, ok := s.byID[id]; ok {
		return r, nil
	}
	return nil, errStubMissing
}

func (s *stubRoleStore) GetByName(name string) (*models.PlatformRole, error) {
	if r, ok := s.byName[name]; ok {
		return r, nil
	}
	return nil, errStubMissing
}

func (s *stubRoleStore) Create(item *models.PlatformRole) error {
	s.created = append(s.created, item)
	return nil
}

func (s *stubRoleStore) Update(item *models.PlatformRole) error {
	s.updated = append(s.updated, item)
	return nil
}

func (s *stubRoleStore) Delete(id uuid.UUID) error {
	s.deleted = append(s.deleted, id)
	return nil
}

func (s *stubRoleStore) CountBindings(uuid.UUID) (int64, error) { return s.bindings, nil }

var validActionsJSON = []byte(`["page:overview:view","org:manage"]`)

func TestPlatformRoleCreate_Validation(t *testing.T) {
	cases := []struct {
		name     string
		roleName string
		actions  []byte
		wantCode string
	}{
		{"blank name", "   ", validActionsJSON, "ERR.10400010"},
		{"empty actions array", "custom", []byte("[]"), "ERR.10400011"},
		{"actions not an array", "custom", []byte(`{"a":1}`), "ERR.10400011"},
		{"actions only blank strings", "custom", []byte(`["  "]`), "ERR.10400011"},
		{"actions absent", "custom", nil, "ERR.10400011"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store := newStubRoleStore()
			svc := NewPlatformRoleService(store)

			err := svc.Create(&models.PlatformRole{Name: tc.roleName, Actions: tc.actions})
			if got := errCode(t, err); got != tc.wantCode {
				t.Fatalf("错误码 = %s，期望 %s", got, tc.wantCode)
			}
			if len(store.created) != 0 {
				t.Fatalf("校验失败却已写库")
			}
		})
	}
}

func TestPlatformRoleCreate_RejectsDuplicateName(t *testing.T) {
	store := newStubRoleStore()
	existing := &models.PlatformRole{ID: uuid.New(), Name: "custom"}
	store.byName["custom"] = existing
	store.byID[existing.ID] = existing
	svc := NewPlatformRoleService(store)

	err := svc.Create(&models.PlatformRole{Name: "custom", Actions: validActionsJSON})
	if got := errCode(t, err); got != "ERR.10409002" {
		t.Fatalf("错误码 = %s，期望 ERR.10409002", got)
	}
	if len(store.created) != 0 {
		t.Fatalf("重名却已写库")
	}
}

// 调用方不能通过 POST 伪造一个内置角色（内置角色不可改不可删）——否则
// 先建一个 isSystem=true 的坑位，后续提升权限的审计就失去意义。
func TestPlatformRoleCreate_CannotForgeBuiltIn(t *testing.T) {
	store := newStubRoleStore()
	svc := NewPlatformRoleService(store)

	in := &models.PlatformRole{Name: "sneaky", Actions: validActionsJSON, IsSystem: true}
	if err := svc.Create(in); err != nil {
		t.Fatalf("应通过，实际 %v", err)
	}
	if in.IsSystem {
		t.Fatalf("IsSystem 必须由服务端置 false，实际 true")
	}
}

func TestPlatformRoleUpdate(t *testing.T) {
	id, org := uuid.New(), uuid.New()
	created := time.Now().Add(-time.Hour)
	newRole := func() *stubRoleStore {
		store := newStubRoleStore()
		r := &models.PlatformRole{ID: id, OrgID: &org, Name: "old", Actions: validActionsJSON, CreatedAt: created}
		store.byID[id] = r
		store.byName["old"] = r
		return store
	}

	t.Run("unknown id maps to 404", func(t *testing.T) {
		svc := NewPlatformRoleService(newStubRoleStore())
		err := svc.Update(uuid.New(), &models.PlatformRole{Name: "x", Actions: validActionsJSON})
		if got := errCode(t, err); got != "ERR.10404001" {
			t.Fatalf("错误码 = %s，期望 ERR.10404001", got)
		}
	})

	// 内置角色可改的话，一次控制台误操作就能把 sdp-admin 的 actions 清空，
	// 结果是所有人（包括管理员自己）都进不去平台。
	t.Run("built-in is immutable", func(t *testing.T) {
		store := newStubRoleStore()
		builtin := &models.PlatformRole{ID: id, Name: "sdp-admin", IsSystem: true, Actions: validActionsJSON}
		store.byID[id] = builtin
		svc := NewPlatformRoleService(store)

		err := svc.Update(id, &models.PlatformRole{Name: "sdp-admin", Actions: validActionsJSON})
		if got := errCode(t, err); got != "ERR.10409003" {
			t.Fatalf("错误码 = %s，期望 ERR.10409003", got)
		}
		if len(store.updated) != 0 {
			t.Fatalf("内置角色被改写")
		}
	})

	t.Run("rename onto another role is a conflict", func(t *testing.T) {
		store := newRole()
		other := &models.PlatformRole{ID: uuid.New(), Name: "taken"}
		store.byName["taken"] = other
		svc := NewPlatformRoleService(store)

		err := svc.Update(id, &models.PlatformRole{Name: "taken", Actions: validActionsJSON})
		if got := errCode(t, err); got != "ERR.10409002" {
			t.Fatalf("错误码 = %s，期望 ERR.10409002", got)
		}
	})

	// Read-modify-write：Save 会写全部映射列，若不带回服务端拥有的字段，
	// org_id / is_system / created_at 会被写成零值。
	t.Run("happy path preserves server-owned fields", func(t *testing.T) {
		store := newRole()
		svc := NewPlatformRoleService(store)

		next := []byte(`["user:manage"]`)
		if err := svc.Update(id, &models.PlatformRole{Name: "new", Description: "d", Actions: next}); err != nil {
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

func TestPlatformRoleDelete(t *testing.T) {
	id := uuid.New()

	t.Run("built-in is immutable", func(t *testing.T) {
		store := newStubRoleStore()
		store.byID[id] = &models.PlatformRole{ID: id, Name: "sdp-admin", IsSystem: true}
		svc := NewPlatformRoleService(store)

		if got := errCode(t, svc.Delete(id)); got != "ERR.10409003" {
			t.Fatalf("错误码 = %s，期望 ERR.10409003", got)
		}
		if len(store.deleted) != 0 {
			t.Fatalf("内置角色被删除")
		}
	})

	// 有绑定引用时拒删（409 + reasons），与仓内其它删除契约同形：
	// 宁可让调用方先清绑定，也不留下指向不存在角色的孤儿行。
	t.Run("in use is refused with reasons", func(t *testing.T) {
		store := newStubRoleStore()
		store.byID[id] = &models.PlatformRole{ID: id, Name: "custom"}
		store.bindings = 3
		svc := NewPlatformRoleService(store)

		err := svc.Delete(id)
		if got := errCode(t, err); got != "ERR.10409004" {
			t.Fatalf("错误码 = %s，期望 ERR.10409004", got)
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
		svc := NewPlatformRoleService(newStubRoleStore())
		if got := errCode(t, svc.Delete(uuid.New())); got != "ERR.10404001" {
			t.Fatalf("错误码 = %s，期望 ERR.10404001", got)
		}
	})

	t.Run("happy path deletes", func(t *testing.T) {
		store := newStubRoleStore()
		store.byID[id] = &models.PlatformRole{ID: id, Name: "custom"}
		svc := NewPlatformRoleService(store)

		if err := svc.Delete(id); err != nil {
			t.Fatalf("应通过，实际 %v", err)
		}
		if len(store.deleted) != 1 || store.deleted[0] != id {
			t.Fatalf("未执行删除：%v", store.deleted)
		}
	})
}
