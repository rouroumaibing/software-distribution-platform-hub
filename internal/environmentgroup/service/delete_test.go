package service

import (
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/rouroumaibing/software-distribution-platform-hub/internal/common"
	"github.com/rouroumaibing/software-distribution-platform-hub/internal/environmentgroup/models"
)

// 覆盖 DELETE-CONTRACT §6.4 #11 / DATA-MODEL §8.4：分组非空 → `409 + {reasons}`；
// 空分组 → 硬删。原实现是 `400` 且不带 `reasons`，与契约的 verdict 形状不一致。

type fakeGroupStore struct {
	members int64
	deleted []uuid.UUID
}

func (f *fakeGroupStore) Create(*models.EnvironmentGroup) error { return nil }

func (f *fakeGroupStore) GetByID(uuid.UUID) (*models.EnvironmentGroup, error) {
	return nil, gorm.ErrRecordNotFound
}

func (f *fakeGroupStore) Update(*models.EnvironmentGroup) error { return nil }

func (f *fakeGroupStore) Delete(id uuid.UUID) error {
	f.deleted = append(f.deleted, id)
	return nil
}

func (f *fakeGroupStore) FindByComponent(uuid.UUID, common.Pagination) ([]models.EnvironmentGroup, int64, error) {
	return nil, 0, nil
}

func (f *fakeGroupStore) CountEnvironments(uuid.UUID) (int64, error) { return f.members, nil }

func TestEnvironmentGroupDelete_RejectsNonEmptyGroup(t *testing.T) {
	store := &fakeGroupStore{members: 2}
	svc := NewEnvironmentGroupService(store)

	err := svc.Delete(uuid.New())

	if err == nil {
		t.Fatalf("want HTTP 409, got nil error")
	}
	ae, ok := err.(*common.APIError)
	if !ok {
		t.Fatalf("want *common.APIError, got %T (%v)", err, err)
	}
	if ae.Code != http.StatusConflict {
		t.Fatalf("want HTTP 409 (contract §6.4 #11), got %d (%s)", ae.Code, ae.ErrorCode)
	}
	if ae.ErrorCode != "ERR.07409002" {
		t.Fatalf("want ERR.07409002, got %s", ae.ErrorCode)
	}
	if len(ae.Reasons) != 1 || !strings.Contains(ae.Reasons[0], "2 个环境") {
		t.Fatalf("reason should carry the member count, got %v", ae.Reasons)
	}
	if len(store.deleted) != 0 {
		t.Fatalf("repo must not be touched when the delete is rejected: %v", store.deleted)
	}
}

func TestEnvironmentGroupDelete_AllowsEmptyGroup(t *testing.T) {
	store := &fakeGroupStore{members: 0}
	svc := NewEnvironmentGroupService(store)
	id := uuid.New()

	if err := svc.Delete(id); err != nil {
		t.Fatalf("empty group must be deletable: %v", err)
	}
	if len(store.deleted) != 1 || store.deleted[0] != id {
		t.Fatalf("want repo delete for %s, got %v", id, store.deleted)
	}
}
