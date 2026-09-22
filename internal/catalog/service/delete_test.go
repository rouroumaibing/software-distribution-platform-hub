package service

import (
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/rouroumaibing/software-distribution-platform-hub/internal/catalog/models"
	"github.com/rouroumaibing/software-distribution-platform-hub/internal/common"
)

// 覆盖 backlog B-12 / DELETE-CONTRACT §6.4 #6（service 层）：有活跃运行 → 409 +
// {reasons}；仅历史运行 → 放行（软删）。service 下的计数由 ActiveRunCounter
// 下钻（组件 → 流水线 → 运行）完成，这里只验 service 层的判定与透传。

type fakeServiceStore struct {
	deleted []uuid.UUID
}

func (f *fakeServiceStore) Create(*models.Service) error { return nil }

func (f *fakeServiceStore) GetByID(uuid.UUID) (*models.Service, error) {
	return nil, gorm.ErrRecordNotFound
}

func (f *fakeServiceStore) Update(*models.Service) error { return nil }

func (f *fakeServiceStore) Delete(id uuid.UUID) error {
	f.deleted = append(f.deleted, id)
	return nil
}

func (f *fakeServiceStore) FindByServiceTreeID(uuid.UUID, common.Pagination) ([]models.Service, int64, error) {
	return nil, 0, nil
}

type fakeServiceRunCounter struct {
	active int64
	err    error
	phases []string
}

func (f *fakeServiceRunCounter) CountActiveByService(_ uuid.UUID, phases []string) (int64, error) {
	f.phases = append([]string(nil), phases...)
	return f.active, f.err
}

func TestServiceDelete_RejectsActiveRuns(t *testing.T) {
	store := &fakeServiceStore{}
	counter := &fakeServiceRunCounter{active: 2}
	// 删除路径不碰 treeLookup（那只服务 ListByOrg），显式传 nil 表示"未装配"。
	svc := NewServiceService(store, counter, nil)

	err := svc.Delete(uuid.New())

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
	if ae.ErrorCode != "ERR.04409001" {
		t.Fatalf("want ERR.04409001, got %s", ae.ErrorCode)
	}
	if len(ae.Reasons) != 1 || !strings.Contains(ae.Reasons[0], "2 条运行") {
		t.Fatalf("reason should carry the active count, got %v", ae.Reasons)
	}
	if len(store.deleted) != 0 {
		t.Fatalf("repo must not be touched when the delete is rejected: %v", store.deleted)
	}
	if len(counter.phases) != 3 {
		t.Fatalf("must count against exactly the active phases, got %v", counter.phases)
	}
}

func TestServiceDelete_AllowsWhenOnlyHistoricalRuns(t *testing.T) {
	store := &fakeServiceStore{}
	svc := NewServiceService(store, &fakeServiceRunCounter{active: 0}, nil)
	id := uuid.New()

	if err := svc.Delete(id); err != nil {
		t.Fatalf("historical-only runs must not block deletion: %v", err)
	}
	if len(store.deleted) != 1 || store.deleted[0] != id {
		t.Fatalf("want repo delete for %s, got %v", id, store.deleted)
	}
}
