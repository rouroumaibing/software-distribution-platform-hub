package service

import (
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/rouroumaibing/software-distribution-platform-hub/internal/common"
	"github.com/rouroumaibing/software-distribution-platform-hub/internal/component/models"
)

// 覆盖 backlog B-12 / DELETE-CONTRACT §6.4 #6 —— 删除的**唯一硬规则**：
// 有活跃运行 → 409 + {reasons}；仅历史运行 → 放行（软删）。
// 全部走内存 fake（ComponentStore / ActiveRunCounter 都是窄接口），不需 Postgres。

type fakeComponentStore struct {
	deleted []uuid.UUID
}

func (f *fakeComponentStore) Create(*models.Component) error { return nil }

func (f *fakeComponentStore) GetByID(uuid.UUID) (*models.Component, error) {
	return nil, gorm.ErrRecordNotFound
}

func (f *fakeComponentStore) Update(*models.Component) error { return nil }

func (f *fakeComponentStore) Delete(id uuid.UUID) error {
	f.deleted = append(f.deleted, id)
	return nil
}

func (f *fakeComponentStore) FindByServiceID(uuid.UUID, common.Pagination) ([]models.Component, int64, error) {
	return nil, 0, nil
}

func (f *fakeComponentStore) GetOrgID(uuid.UUID) (uuid.UUID, error) { return uuid.Nil, nil }

// fakeActiveRunCounter 记录传入的 phase 集合，把"只按活跃 phase 计数"钉在代码里，
// 而不只是留在注释里。
type fakeActiveRunCounter struct {
	active int64
	err    error
	phases []string
}

func (f *fakeActiveRunCounter) CountActiveByComponent(_ uuid.UUID, phases []string) (int64, error) {
	f.phases = append([]string(nil), phases...)
	return f.active, f.err
}

// CountActiveByComponentTx 实现 ActiveRunCounter 的 tx 视图；fake 下与无 tx 版本
// 同语义（记录 phase、返回同一 active 数），供级联事务内的 guard 复检使用。
func (f *fakeActiveRunCounter) CountActiveByComponentTx(_ *gorm.DB, _ uuid.UUID, phases []string) (int64, error) {
	f.phases = append([]string(nil), phases...)
	return f.active, f.err
}

func assertConflictWithReasons(t *testing.T, err error, wantCode string) *common.APIError {
	t.Helper()
	if err == nil {
		t.Fatalf("want HTTP 409, got nil error")
	}
	ae, ok := err.(*common.APIError)
	if !ok {
		t.Fatalf("want *common.APIError, got %T (%v)", err, err)
	}
	if ae.Code != http.StatusConflict {
		t.Fatalf("want HTTP 409, got %d (%s)", ae.Code, ae.ErrorCode)
	}
	if wantCode != "" && ae.ErrorCode != wantCode {
		t.Fatalf("want errorCode %s, got %s", wantCode, ae.ErrorCode)
	}
	if len(ae.Reasons) == 0 {
		t.Fatalf("409 must carry reasons for the console verdict (DELETE-CONTRACT §2.2)")
	}
	return ae
}

func TestComponentDelete_RejectsActiveRuns(t *testing.T) {
	store := &fakeComponentStore{}
	counter := &fakeActiveRunCounter{active: 3}
	svc := NewComponentService(store, nil, nil, counter)

	err := svc.Delete(uuid.New())

	ae := assertConflictWithReasons(t, err, "ERR.05409001")
	if !strings.Contains(ae.Reasons[0], "3 条运行") {
		t.Fatalf("reason should carry the active count, got %q", ae.Reasons[0])
	}
	if len(store.deleted) != 0 {
		t.Fatalf("repo must not be touched when the delete is rejected: %v", store.deleted)
	}
	if len(counter.phases) != 3 {
		t.Fatalf("must count against exactly the active phases, got %v", counter.phases)
	}
}

func TestComponentDelete_AllowsWhenOnlyHistoricalRuns(t *testing.T) {
	store := &fakeComponentStore{}
	svc := NewComponentService(store, nil, nil, &fakeActiveRunCounter{active: 0})
	id := uuid.New()

	if err := svc.Delete(id); err != nil {
		t.Fatalf("historical-only runs must not block deletion: %v", err)
	}
	if len(store.deleted) != 1 || store.deleted[0] != id {
		t.Fatalf("want repo delete for %s, got %v", id, store.deleted)
	}
}

func TestComponentDelete_NoCounterStillDeletes(t *testing.T) {
	store := &fakeComponentStore{}
	svc := NewComponentService(store, nil, nil, nil) // 未装配计数器的装配路径

	if err := svc.Delete(uuid.New()); err != nil {
		t.Fatalf("delete without a counter wiring must not fail: %v", err)
	}
	if len(store.deleted) != 1 {
		t.Fatalf("want 1 delete, got %d", len(store.deleted))
	}
}
