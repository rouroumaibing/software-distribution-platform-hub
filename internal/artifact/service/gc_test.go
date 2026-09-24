package service

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/rouroumaibing/software-distribution-platform-hub/internal/artifact/models"
	"github.com/rouroumaibing/software-distribution-platform-hub/internal/artifact/storage"
)

// 覆盖 backlog B-16 的**保留期 GC**（`expires_at` 生效 + `cleanup_state` 可重试）。
//
// 四条要钉住的性质：
//  1. 顺序是**先对象、后行** —— 反过来（先删行）对象删失败就没行可重试了；
//  2. 对象删失败 ⇒ 行**保留**并标记 pending，且**不算删成功**；
//  3. 没配对象存储时元数据 GC 照跑（删行），且不产生任何对象调用；
//  4. GC 只有删除能力、**没有列举能力**（结构上就不可能扫桶）。

// ---- fakes -----------------------------------------------------------------

type fakeArtifactRepo struct {
	expired   []models.Artifact
	findErr   error
	markErr   error
	deleteErr error

	gotNow    time.Time
	gotLimit  int
	findCalls int
	pending   []uuid.UUID
	deleted   []uuid.UUID
}

func (f *fakeArtifactRepo) FindExpired(now time.Time, limit int) ([]models.Artifact, error) {
	f.findCalls++
	f.gotNow, f.gotLimit = now, limit
	if f.findErr != nil {
		return nil, f.findErr
	}
	return f.expired, nil
}

func (f *fakeArtifactRepo) MarkCleanupPending(id uuid.UUID) error {
	if f.markErr != nil {
		return f.markErr
	}
	f.pending = append(f.pending, id)
	return nil
}

func (f *fakeArtifactRepo) Delete(id uuid.UUID) error {
	if f.deleteErr != nil {
		return f.deleteErr
	}
	f.deleted = append(f.deleted, id)
	return nil
}

type fakeObjectDeleter struct {
	calls []string
	errOn map[string]error
}

func (f *fakeObjectDeleter) Delete(key string) error {
	f.calls = append(f.calls, key)
	if f.errOn != nil {
		if err, ok := f.errOn[key]; ok {
			return err
		}
	}
	return nil
}

func artifact(id uuid.UUID, key string) models.Artifact {
	return models.Artifact{ID: id, ComponentID: uuid.New(), StorageKey: key, Version: "v1"}
}

// ---- 结构保证：GC 不会扫桶 --------------------------------------------------

// GC 的 store 面只有 Delete。若它将来被并进 storage.Client（带 Presign*），
// 或错误地收下 Enumerator，"GC 只删它被指着的那一个 key" 这条性质就静默消失了。
func TestArtifactGC_StoreSurfaceCannotEnumerate(t *testing.T) {
	if _, canList := any(&fakeEnumerator{}).(ObjectDeleter); canList {
		t.Fatalf("GC must not accept a listing capabibility — it acts only on rows handed to it")
	}
	if _, canDelete := any(&fakeEnumerator{}).(storage.Client); canDelete {
		t.Fatalf("sanity: the listing fake must not satisfy storage.Client either")
	}
}

// ---- RunOnce 编排 -----------------------------------------------------------

func TestArtifactGC_DeletesObjectThenRowForEachExpired(t *testing.T) {
	a, b := uuid.New(), uuid.New()
	repo := &fakeArtifactRepo{expired: []models.Artifact{
		artifact(a, "components/1/v1/a.tar"),
		artifact(b, "components/1/v1/b.tar"),
	}}
	store := &fakeObjectDeleter{}
	gc := NewArtifactGC(repo, store, 10)

	now := time.Unix(1_700_000_000, 0)
	rep, err := gc.RunOnce(now)
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if rep.Scanned != 2 || rep.Deleted != 2 || rep.ObjectsFailed != 0 || rep.RowsFailed != 0 {
		t.Fatalf("report wrong: %+v", rep)
	}
	if !reflect.DeepEqual(store.calls, []string{"components/1/v1/a.tar", "components/1/v1/b.tar"}) {
		t.Fatalf("objects must be deleted in row order: %v", store.calls)
	}
	if !reflect.DeepEqual(repo.deleted, []uuid.UUID{a, b}) {
		t.Fatalf("rows must be dropped after their object: %v", repo.deleted)
	}
	if len(repo.pending) != 0 {
		t.Fatalf("nothing failed, so nothing may be marked pending: %v", repo.pending)
	}
	if repo.gotLimit != 10 {
		t.Fatalf("batch must bound the pass, got limit=%d", repo.gotLimit)
	}
	if !repo.gotNow.Equal(now) {
		t.Fatalf("the pass must use the supplied clock, got %v", repo.gotNow)
	}
}

func TestArtifactGC_ObjectFailureRetainsRowAndMarksPending(t *testing.T) {
	good, bad := uuid.New(), uuid.New()
	repo := &fakeArtifactRepo{expired: []models.Artifact{
		artifact(good, "components/1/v1/good.tar"),
		artifact(bad, "components/1/v1/bad.tar"),
	}}
	store := &fakeObjectDeleter{errOn: map[string]error{
		"components/1/v1/bad.tar": errors.New("access denied"),
	}}
	gc := NewArtifactGC(repo, store, 10)

	rep, err := gc.RunOnce(time.Now())
	if err != nil {
		t.Fatalf("a per-object failure must not fail the pass: %v", err)
	}
	if rep.Deleted != 1 || rep.ObjectsFailed != 1 {
		t.Fatalf("report wrong: %+v", rep)
	}
	if !reflect.DeepEqual(rep.PendingSample, []string{"components/1/v1/bad.tar"}) {
		t.Fatalf("failing key must be surfaced: %v", rep.PendingSample)
	}
	// 关键：失败行**不能**被删 —— 删了就没有行可用于重试。
	if !reflect.DeepEqual(repo.deleted, []uuid.UUID{good}) {
		t.Fatalf("only the healthy row may be dropped: %v", repo.deleted)
	}
	if !reflect.DeepEqual(repo.pending, []uuid.UUID{bad}) {
		t.Fatalf("the failing row must be marked pending (durable retry): %v", repo.pending)
	}
	if !rep.Failed() {
		t.Fatalf("a pass with an object failure must report Failed()")
	}
}

// 重试路径：上一轮标记 pending 的行这一轮被重新选中、对象这次删成功 ⇒ 行被删掉。
// 这正是 cleanup_state 存在的意义（自愈，而不是等人读报告）。
func TestArtifactGC_RetryOfPendingRowSucceedsAndDropsIt(t *testing.T) {
	retry := uuid.New()
	repo := &fakeArtifactRepo{expired: []models.Artifact{{
		ID: retry, ComponentID: uuid.New(), StorageKey: "components/1/v1/retry.tar",
		CleanupState: models.CleanupStatePendingDeletion,
	}}}
	store := &fakeObjectDeleter{} // 这次不失败
	gc := NewArtifactGC(repo, store, 10)

	rep, err := gc.RunOnce(time.Now())
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if rep.Deleted != 1 || rep.ObjectsFailed != 0 {
		t.Fatalf("retry must complete the deletion: %+v", rep)
	}
	if !reflect.DeepEqual(repo.deleted, []uuid.UUID{retry}) {
		t.Fatalf("the retried row must now be dropped: %v", repo.deleted)
	}
}

// 没配对象存储：元数据 GC 仍然有效（删行），且**一次对象调用都不发**。
func TestArtifactGC_NoStoreConfiguredStillDeletesRows(t *testing.T) {
	a := uuid.New()
	repo := &fakeArtifactRepo{expired: []models.Artifact{artifact(a, "k")}}
	gc := NewArtifactGC(repo, nil, 10)

	rep, err := gc.RunOnce(time.Now())
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if rep.Deleted != 1 || rep.ObjectsFailed != 0 {
		t.Fatalf("metadata GC must work without a store: %+v", rep)
	}
	if !reflect.DeepEqual(repo.deleted, []uuid.UUID{a}) {
		t.Fatalf("row must still be dropped: %v", repo.deleted)
	}
	if len(repo.pending) != 0 {
		t.Fatalf("without a store there is no object failure to mark: %v", repo.pending)
	}
}

// 行删除失败：不改报成功，且**继续处理同一批的其余行**（一个坏行不该卡住整批）。
func TestArtifactGC_MetadataDeleteFailureIsCountedNotSwallowed(t *testing.T) {
	repo := &fakeArtifactRepo{
		expired:   []models.Artifact{artifact(uuid.New(), "k")},
		deleteErr: errors.New("db down"),
	}
	store := &fakeObjectDeleter{}
	gc := NewArtifactGC(repo, store, 10)

	rep, err := gc.RunOnce(time.Now())
	if err != nil {
		t.Fatalf("a row failure must not fail the whole pass: %v", err)
	}
	if rep.Deleted != 0 || rep.RowsFailed != 1 {
		t.Fatalf("a failed row delete must be counted, not reported as deleted: %+v", rep)
	}
	if !rep.Failed() {
		t.Fatalf("RowsFailed must make Failed() true")
	}
}

func TestArtifactGC_PropagatesFindError(t *testing.T) {
	repo := &fakeArtifactRepo{findErr: errors.New("db down")}
	gc := NewArtifactGC(repo, &fakeObjectDeleter{}, 10)
	if _, err := gc.RunOnce(time.Now()); err == nil {
		t.Fatalf("a lookup failure must surface (otherwise it looks like 'nothing expired')")
	}
}

// batch<=0 走默认；显式 batch 原样传给仓储（批量闸门不能被悄悄改成无界）。
func TestArtifactGC_BatchDefaultAndOverride(t *testing.T) {
	repo := &fakeArtifactRepo{}
	if _, err := NewArtifactGC(repo, nil, 0).RunOnce(time.Now()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if repo.gotLimit != DefaultArtifactGCBatch {
		t.Fatalf("batch<=0 must fall back to %d, got %d", DefaultArtifactGCBatch, repo.gotLimit)
	}

	repo2 := &fakeArtifactRepo{}
	if _, err := NewArtifactGC(repo2, nil, 7).RunOnce(time.Now()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if repo2.gotLimit != 7 {
		t.Fatalf("explicit batch must be honoured, got %d", repo2.gotLimit)
	}
}

// 失败样本有上限：一个坏批不该把日志撑成无界（counts 仍然精确）。
func TestArtifactGC_PendingSampleIsBounded(t *testing.T) {
	errOn := map[string]error{}
	var rows []models.Artifact
	for i := 0; i < MaxGCSample+5; i++ {
		key := "components/1/v1/k" + string(rune('a'+i%26)) + string(rune('0'+i/26))
		errOn[key] = errors.New("boom")
		rows = append(rows, artifact(uuid.New(), key))
	}
	repo := &fakeArtifactRepo{expired: rows}
	gc := NewArtifactGC(repo, &fakeObjectDeleter{errOn: errOn}, 100)

	rep, err := gc.RunOnce(time.Now())
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if rep.ObjectsFailed != len(rows) {
		t.Fatalf("count must stay exact: want %d, got %d", len(rows), rep.ObjectsFailed)
	}
	if len(rep.PendingSample) != MaxGCSample {
		t.Fatalf("sample must be capped at %d, got %d", MaxGCSample, len(rep.PendingSample))
	}
}

// interval<=0 = 关闭：不得启动 ticker，也不得跑预备轮（会删数据的作业不能由部署悄悄打开）。
func TestArtifactGC_RunDisabledByZeroInterval(t *testing.T) {
	repo := &fakeArtifactRepo{expired: []models.Artifact{artifact(uuid.New(), "k")}}
	gc := NewArtifactGC(repo, &fakeObjectDeleter{}, 10)
	gc.Run(context.Background(), 0)
	if repo.findCalls != 0 {
		t.Fatalf("disabled GC must not touch the repo, got %d lookups", repo.findCalls)
	}
}
