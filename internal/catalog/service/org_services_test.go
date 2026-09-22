package service

import (
	"net/http"
	"testing"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/rouroumaibing/software-distribution-platform-hub/internal/catalog/models"
	"github.com/rouroumaibing/software-distribution-platform-hub/internal/common"
)

// 覆盖附 A N-9 的 `GET /orgs/:id/services`（服务树页懒加载的直接子层端点）。
// 三件事必须钉住：① 组织 id 真的被解析成树 id（而不是把 org id 当 tree id 用）；
// ② 组织不存在时错误**向上抛**（交给 handler 变 404/500），不静默返回空列表 ——
// 静默空列表会让"组织不存在"和"组织下没有服务"在 UI 上长得一模一样；
// ③ 漏装配依赖时报 500 而不是 nil panic。

type fakeTreeLookup struct {
	treeID uuid.UUID
	err    error
	gotOrg uuid.UUID
	calls  int
}

func (f *fakeTreeLookup) ServiceTreeIDByOrg(orgID uuid.UUID) (uuid.UUID, error) {
	f.calls++
	f.gotOrg = orgID
	return f.treeID, f.err
}

type fakeListingStore struct {
	gotTreeID uuid.UUID
	gotPaging common.Pagination
	items     []models.Service
	total     int64
	err       error
}

func (f *fakeListingStore) Create(*models.Service) error { return nil }
func (f *fakeListingStore) GetByID(uuid.UUID) (*models.Service, error) {
	return nil, gorm.ErrRecordNotFound
}
func (f *fakeListingStore) Update(*models.Service) error { return nil }
func (f *fakeListingStore) Delete(uuid.UUID) error       { return nil }

func (f *fakeListingStore) FindByServiceTreeID(treeID uuid.UUID, p common.Pagination) ([]models.Service, int64, error) {
	f.gotTreeID = treeID
	f.gotPaging = p
	return f.items, f.total, f.err
}

func TestListByOrg_ResolvesTreeThenListsChildren(t *testing.T) {
	orgID := uuid.New()
	treeID := uuid.New()
	lookup := &fakeTreeLookup{treeID: treeID}
	store := &fakeListingStore{
		items: []models.Service{{Key: "user-center", Name: "用户中心"}},
		total: 1,
	}
	svc := NewServiceService(store, nil, lookup)

	items, total, err := svc.ListByOrg(orgID, common.Pagination{Page: 1, PageSize: 50})
	if err != nil {
		t.Fatalf("want success, got %v", err)
	}
	if lookup.gotOrg != orgID {
		t.Fatalf("lookup got org %s, want %s", lookup.gotOrg, orgID)
	}
	if store.gotTreeID != treeID {
		t.Fatalf("repo got tree %s, want the resolved %s (org id must not leak through)", store.gotTreeID, treeID)
	}
	if store.gotPaging.PageSize != 50 {
		t.Fatalf("pagination must be forwarded verbatim, got %+v", store.gotPaging)
	}
	if total != 1 || len(items) != 1 || items[0].Key != "user-center" {
		t.Fatalf("unexpected listing result: total=%d items=%+v", total, items)
	}
}

func TestListByOrg_PropagatesUnknownOrgInsteadOfEmptyList(t *testing.T) {
	store := &fakeListingStore{items: nil, total: 0}
	svc := NewServiceService(store, nil, &fakeTreeLookup{err: gorm.ErrRecordNotFound})

	if _, _, err := svc.ListByOrg(uuid.New(), common.Pagination{Page: 1, PageSize: 20}); err == nil {
		t.Fatalf("unknown org must surface as an error, not an empty page")
	}
	if store.gotTreeID != uuid.Nil {
		t.Fatalf("repo must not be queried when the tree cannot be resolved, got tree %s", store.gotTreeID)
	}
}

func TestListByOrg_MissingLookupFailsLoudly(t *testing.T) {
	svc := NewServiceService(&fakeListingStore{}, nil, nil)

	_, _, err := svc.ListByOrg(uuid.New(), common.Pagination{Page: 1, PageSize: 20})
	ae, ok := err.(*common.APIError)
	if !ok {
		t.Fatalf("want *common.APIError, got %T (%v)", err, err)
	}
	if ae.Code != http.StatusInternalServerError {
		t.Fatalf("want 500, got %d", ae.Code)
	}
}
