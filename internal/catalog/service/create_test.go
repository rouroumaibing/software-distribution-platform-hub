package service

import (
	"net/http"
	"testing"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/rouroumaibing/software-distribution-platform-hub/internal/catalog/models"
	"github.com/rouroumaibing/software-distribution-platform-hub/internal/common"
)

// D-02：POST /services 必须拒绝不存在的 serviceTreeId，不能让悬挂引用入库。

type createStore struct {
	createCalled bool
}

func (s *createStore) Create(*models.Service) error { s.createCalled = true; return nil }
func (s *createStore) GetByID(uuid.UUID) (*models.Service, error) {
	return nil, gorm.ErrRecordNotFound
}
func (s *createStore) Update(*models.Service) error { return nil }
func (s *createStore) Delete(uuid.UUID) error       { return nil }
func (s *createStore) FindByServiceTreeID(uuid.UUID, common.Pagination) ([]models.Service, int64, error) {
	return nil, 0, nil
}

type createTreeLookup struct {
	exists bool
	err    error
}

func (l *createTreeLookup) ServiceTreeIDByOrg(orgID uuid.UUID) (uuid.UUID, error) {
	return uuid.Nil, nil
}
func (l *createTreeLookup) ServiceTreeExists(treeID uuid.UUID) (bool, error) {
	return l.exists, l.err
}

func TestCreate_RejectsUnknownServiceTree(t *testing.T) {
	store := &createStore{}
	svc := NewServiceService(store, nil, &createTreeLookup{exists: false})

	err := svc.Create(&models.Service{ServiceTreeID: uuid.New(), Key: "svc-x", Name: "Svc X"})
	ae, ok := err.(*common.APIError)
	if !ok {
		t.Fatalf("want *common.APIError, got %T (%v)", err, err)
	}
	if ae.Code != http.StatusBadRequest {
		t.Fatalf("want 400 for dangling serviceTreeId, got %d", ae.Code)
	}
	if store.createCalled {
		t.Fatalf("repo.Create must not run when the referenced service tree is missing")
	}
}

func TestCreate_AcceptsLiveServiceTree(t *testing.T) {
	store := &createStore{}
	svc := NewServiceService(store, nil, &createTreeLookup{exists: true})

	if err := svc.Create(&models.Service{ServiceTreeID: uuid.New(), Key: "svc-y", Name: "Svc Y"}); err != nil {
		t.Fatalf("want success for a live service tree, got %v", err)
	}
	if !store.createCalled {
		t.Fatalf("repo.Create must run once the tree exists")
	}
}
