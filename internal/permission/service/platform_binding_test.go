package service

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/rouroumaibing/software-distribution-platform-hub/internal/common"
	"github.com/rouroumaibing/software-distribution-platform-hub/internal/permission/models"
)

// stubBindingStore records what the service actually did, so a test can assert
// not only the returned error but also that a rejected request never reached
// persistence (the hub's deletion tests use the same "不碰 repo" assertion).
type stubBindingStore struct {
	items    []models.PlatformRoleBinding
	created  []*models.PlatformRoleBinding
	deleted  []uuid.UUID
	exists   bool
	listErr  error
	getErr   error
	lastList struct {
		orgID       *uuid.UUID
		subjectType string
		subjectID   string
	}
}

func (s *stubBindingStore) List(orgID *uuid.UUID, subjectType, subjectID string) ([]models.PlatformRoleBinding, error) {
	s.lastList.orgID, s.lastList.subjectType, s.lastList.subjectID = orgID, subjectType, subjectID
	return s.items, s.listErr
}

func (s *stubBindingStore) GetByID(id uuid.UUID) (*models.PlatformRoleBinding, error) {
	if s.getErr != nil {
		return nil, s.getErr
	}
	return &models.PlatformRoleBinding{ID: id}, nil
}

func (s *stubBindingStore) Create(item *models.PlatformRoleBinding) error {
	s.created = append(s.created, item)
	return nil
}

func (s *stubBindingStore) Delete(id uuid.UUID) error {
	s.deleted = append(s.deleted, id)
	return nil
}

func (s *stubBindingStore) ExistsActive(*uuid.UUID, string, string, uuid.UUID) (bool, error) {
	return s.exists, nil
}

type stubRoleLookup struct{ found bool }

func (s stubRoleLookup) GetByID(id uuid.UUID) (*models.PlatformRole, error) {
	if !s.found {
		return nil, common.NewAPIError(common.KindPermission, 404, 1, "not found")
	}
	return &models.PlatformRole{ID: id}, nil
}

func errCode(t *testing.T, err error) string {
	t.Helper()
	if err == nil {
		t.Fatalf("期望报错，实际 nil")
	}
	ae, ok := err.(*common.APIError)
	if !ok {
		t.Fatalf("期望 *common.APIError，实际 %T: %v", err, err)
	}
	return ae.ErrorCode
}

// §5.3 的主体语义 + D1 的保留前缀，全部在服务层拦下（应用层校验挡不住手工
// SQL，所以 DDL 里另有一份 CHECK；这里测的是 API 路径）。
func TestPlatformBindingCreate_RejectsInvalidSubject(t *testing.T) {
	cases := []struct {
		name        string
		subjectType string
		subjectID   string
		wantCode    string
	}{
		{"user missing id", "user", "", "ERR.10400021"},
		{"group missing id", "group", "", "ERR.10400021"},
		{"unknown type", "robot", "sub-1", "ERR.10400020"},
		{"empty type", "", "sub-1", "ERR.10400020"},
		// §5.3 的反例：claim 是 /sdp-admins（full.path=true），写成 sdp-admins
		// 会**静默不匹配** —— 这里必须变成 400 而不是存下一行永远不生效的绑定。
		{"group without leading slash", "group", "sdp-admins", "ERR.10400022"},
		// D1 硬约定：/org: 是组织维度，不得作 RBAC 主体（§2.3）。
		{"group under reserved org prefix", "group", "/org:acme", "ERR.10400023"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store := &stubBindingStore{}
			svc := NewPlatformBindingService(store, stubRoleLookup{found: true})

			err := svc.Create(&models.PlatformRoleBinding{
				SubjectType:    tc.subjectType,
				SubjectID:      tc.subjectID,
				PlatformRoleID: uuid.New(),
			})

			if got := errCode(t, err); got != tc.wantCode {
				t.Fatalf("错误码 = %s，期望 %s", got, tc.wantCode)
			}
			if len(store.created) != 0 {
				t.Fatalf("校验失败却已写库：%d 行", len(store.created))
			}
		})
	}
}

func TestPlatformBindingCreate_TrimsSubjectAndPersists(t *testing.T) {
	for _, tc := range []struct{ st, id, want string }{
		{"user", "  sub-1  ", "sub-1"},
		{"group", "/sdp-admin", "/sdp-admin"},
	} {
		store := &stubBindingStore{}
		svc := NewPlatformBindingService(store, stubRoleLookup{found: true})

		in := &models.PlatformRoleBinding{SubjectType: tc.st, SubjectID: tc.id, PlatformRoleID: uuid.New()}
		if err := svc.Create(in); err != nil {
			t.Fatalf("%s/%s 应通过，实际 %v", tc.st, tc.id, err)
		}
		if len(store.created) != 1 {
			t.Fatalf("应写入 1 行，实际 %d", len(store.created))
		}
		if in.SubjectType != tc.st || in.SubjectID != tc.want {
			t.Fatalf("写入值为 %s/%s，期望 %s/%s", in.SubjectType, in.SubjectID, tc.st, tc.want)
		}
	}
}

func TestPlatformBindingCreate_RequiresExistingRole(t *testing.T) {
	// 角色 id 缺失 / 角色不存在必须报错，否则会落下一行指向不存在角色的绑定：
	// 它永远解不出任何 action，表现为"授予了但没权限"，最难排查。
	t.Run("nil role id", func(t *testing.T) {
		store := &stubBindingStore{}
		svc := NewPlatformBindingService(store, stubRoleLookup{found: true})
		err := svc.Create(&models.PlatformRoleBinding{SubjectType: "user", SubjectID: "sub-1"})
		if got := errCode(t, err); got != "ERR.10400024" {
			t.Fatalf("错误码 = %s，期望 ERR.10400024", got)
		}
		if len(store.created) != 0 {
			t.Fatalf("校验失败却已写库")
		}
	})

	t.Run("role not found", func(t *testing.T) {
		store := &stubBindingStore{}
		svc := NewPlatformBindingService(store, stubRoleLookup{found: false})
		err := svc.Create(&models.PlatformRoleBinding{SubjectType: "user", SubjectID: "sub-1", PlatformRoleID: uuid.New()})
		if got := errCode(t, err); got != "ERR.10404001" {
			t.Fatalf("错误码 = %s，期望 ERR.10404001", got)
		}
		if len(store.created) != 0 {
			t.Fatalf("校验失败却已写库")
		}
	})
}

func TestPlatformBindingCreate_Expiry(t *testing.T) {
	t.Run("past expiry rejected", func(t *testing.T) {
		store := &stubBindingStore{}
		svc := NewPlatformBindingService(store, stubRoleLookup{found: true})
		past := time.Now().Add(-time.Minute)
		err := svc.Create(&models.PlatformRoleBinding{
			SubjectType: "user", SubjectID: "sub-1", PlatformRoleID: uuid.New(), ExpiresAt: &past,
		})
		if got := errCode(t, err); got != "ERR.10400025" {
			t.Fatalf("错误码 = %s，期望 ERR.10400025", got)
		}
		if len(store.created) != 0 {
			t.Fatalf("校验失败却已写库")
		}
	})

	t.Run("future expiry accepted, nil expiry accepted", func(t *testing.T) {
		for _, exp := range []*time.Time{nil, ptrTime(time.Now().Add(time.Hour))} {
			store := &stubBindingStore{}
			svc := NewPlatformBindingService(store, stubRoleLookup{found: true})
			err := svc.Create(&models.PlatformRoleBinding{
				SubjectType: "user", SubjectID: "sub-1", PlatformRoleID: uuid.New(), ExpiresAt: exp,
			})
			if err != nil {
				t.Fatalf("应通过，实际 %v", err)
			}
			if len(store.created) != 1 {
				t.Fatalf("应写入 1 行")
			}
		}
	})
}

func TestPlatformBindingCreate_RejectsDuplicateActiveGrant(t *testing.T) {
	store := &stubBindingStore{exists: true}
	svc := NewPlatformBindingService(store, stubRoleLookup{found: true})
	err := svc.Create(&models.PlatformRoleBinding{SubjectType: "user", SubjectID: "sub-1", PlatformRoleID: uuid.New()})
	if got := errCode(t, err); got != "ERR.10409005" {
		t.Fatalf("错误码 = %s，期望 ERR.10409005", got)
	}
	if len(store.created) != 0 {
		t.Fatalf("重复绑定却已写库")
	}
}

func TestPlatformBindingList_RejectsBogusSubjectType(t *testing.T) {
	// 过滤条件打错字必须报 400，否则会返回空列表 —— 在管理页面上
	// 表现为"绑定都没了"，比报错危险。
	store := &stubBindingStore{}
	svc := NewPlatformBindingService(store, stubRoleLookup{found: true})
	_, lerr := svc.List(nil, "User", "")
	if got := errCode(t, lerr); got != "ERR.10400020" {
		t.Fatalf("错误码 = %s，期望 ERR.10400020", got)
	}

	// 合法过滤（含空过滤）应透传，且两侧空白被裁掉。
	if _, err := svc.List(nil, "  group  ", " /sdp-admin "); err != nil {
		t.Fatalf("合法过滤不应报错：%v", err)
	}
	if store.lastList.subjectType != "group" || store.lastList.subjectID != "/sdp-admin" {
		t.Fatalf("过滤值未裁剪：%+v", store.lastList)
	}
}

func TestPlatformBindingDelete_MapsMissingTo404(t *testing.T) {
	store := &stubBindingStore{getErr: common.NewAPIError(common.KindBase, 404, 1, "gone")}
	svc := NewPlatformBindingService(store, stubRoleLookup{found: true})
	if got := errCode(t, svc.Delete(uuid.New())); got != "ERR.10404002" {
		t.Fatalf("错误码 = %s，期望 ERR.10404002", got)
	}
	if len(store.deleted) != 0 {
		t.Fatalf("目标不存在却执行了删除")
	}
}

func ptrTime(t time.Time) *time.Time { return &t }
