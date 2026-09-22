package service

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/rouroumaibing/software-distribution-platform-hub/internal/artifact/storage"
)

// 覆盖 backlog B-16 的**孤儿对象对账（仅报告）**。
//
// 三条要钉住的性质：
//  1. 差异判定是纯函数、与 DB / 对象存储无关，可独立测；
//  2. 对账**没有任何删除能力** —— 结构上就不成立（Enumerator 上只有列举）；
//  3. 对象存储**不支持列举**时不能被当成"很干净"，必须显式区分。

// ---- 纯差异判定 -----------------------------------------------------------

func TestDiffStorageKeys(t *testing.T) {
	cases := []struct {
		name       string
		db         []string
		objects    []string
		wantMiss   []string
		wantOrphan []string
	}{
		{
			name:       "两边一致则都为空",
			db:         []string{"a", "b"},
			objects:    []string{"a", "b"},
			wantMiss:   []string{},
			wantOrphan: []string{},
		},
		{
			name:       "DB 有对象没有 = 缺对象（下载必 404）",
			db:         []string{"a", "b"},
			objects:    []string{"a"},
			wantMiss:   []string{"b"},
			wantOrphan: []string{},
		},
		{
			name:       "对象有 DB 没有 = 孤儿对象（不可达残留）",
			db:         []string{"a"},
			objects:    []string{"a", "c"},
			wantMiss:   []string{},
			wantOrphan: []string{"c"},
		},
		{
			name:       "两侧都空",
			db:         nil,
			objects:    nil,
			wantMiss:   []string{},
			wantOrphan: []string{},
		},
		{
			name:       "结果按 key 升序（报告要可比对）",
			db:         []string{"z", "a"},
			objects:    []string{"y", "b"},
			wantMiss:   []string{"a", "z"},
			wantOrphan: []string{"b", "y"},
		},
		{
			name:       "空字符串 key 是坏数据，不参与比对",
			db:         []string{"", "a"},
			objects:    []string{"", "a"},
			wantMiss:   []string{},
			wantOrphan: []string{},
		},
		{
			name:       "重复 key 收敛（DB 允许同 key 多行时不该报两次）",
			db:         []string{"a", "a"},
			objects:    []string{"a"},
			wantMiss:   []string{},
			wantOrphan: []string{},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			miss, orphan := DiffStorageKeys(tc.db, tc.objects)
			if !reflect.DeepEqual(miss, tc.wantMiss) {
				t.Fatalf("missing: want %v, got %v", tc.wantMiss, miss)
			}
			if !reflect.DeepEqual(orphan, tc.wantOrphan) {
				t.Fatalf("orphan: want %v, got %v", tc.wantOrphan, orphan)
			}
		})
	}
}

// ---- 结构保证：对账不可能删东西 --------------------------------------------

// 这是本题最该钉住的一点。若 Enumerator 将来被并回 Client（带 Delete），
// 一个"仅报告"的作业就具备了删除能力 —— 注释里的承诺会静默失效。
func TestEnumerator_HasNoMutationSurface(t *testing.T) {
	fake := &fakeEnumerator{}
	if _, isClient := any(fake).(storage.Client); isClient {
		t.Fatalf("the listing surface must not satisfy storage.Client (which can Delete)")
	}
}

// ---- 编排（fake）+ 本地驱动（真实文件系统） --------------------------------

type fakeKeySource struct {
	keys []string
	err  error
}

func (f *fakeKeySource) ListStorageKeys() ([]string, error) { return f.keys, f.err }

type fakeEnumerator struct {
	objects   []storage.ObjectInfo
	truncated bool
	err       error
	gotPrefix string
	gotLimit  int
}

func (f *fakeEnumerator) ListObjects(prefix string, limit int) ([]storage.ObjectInfo, bool, error) {
	f.gotPrefix, f.gotLimit = prefix, limit
	return f.objects, f.truncated, f.err
}

func TestReconciler_ReportsBothSidesWithCountsAndSamples(t *testing.T) {
	store := &fakeEnumerator{objects: []storage.ObjectInfo{
		{Key: "components/1/v1/a.tar"},
		{Key: "components/1/v1/orphan.tar"},
	}}
	rec := NewArtifactReconciler(&fakeKeySource{keys: []string{
		"components/1/v1/a.tar",
		"components/1/v1/missing.tar",
	}}, store, "components/")

	rep, err := rec.RunOnce(time.Unix(1700000000, 0))
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if rep.MissingObjects != 1 || rep.OrphanObjects != 1 {
		t.Fatalf("counts wrong: %+v", rep)
	}
	if !reflect.DeepEqual(rep.MissingSample, []string{"components/1/v1/missing.tar"}) {
		t.Fatalf("missing sample wrong: %v", rep.MissingSample)
	}
	if !reflect.DeepEqual(rep.OrphanSample, []string{"components/1/v1/orphan.tar"}) {
		t.Fatalf("orphan sample wrong: %v", rep.OrphanSample)
	}
	if rep.Clean() {
		t.Fatalf("report with orphans must not be Clean()")
	}
	if store.gotPrefix != "components/" || store.gotLimit != DefaultMaxObjects {
		t.Fatalf("the listing must be scoped and bounded: prefix=%q limit=%d", store.gotPrefix, store.gotLimit)
	}
}

func TestReconciler_CleanWhenBothSidesMatch(t *testing.T) {
	store := &fakeEnumerator{objects: []storage.ObjectInfo{{Key: "k"}}}
	rec := NewArtifactReconciler(&fakeKeySource{keys: []string{"k"}}, store, "")
	rep, err := rec.RunOnce(time.Now())
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if !rep.Clean() {
		t.Fatalf("matching sets must be clean: %+v", rep)
	}
	if rep.DBKeys != 1 || rep.ObjectKeys != 1 {
		t.Fatalf("sizes not reported: %+v", rep)
	}
}

// 列举被上限截断时必须显式标注 —— 否则读者会把"扫了一部分"当成"全量对账"。
func TestReconciler_ReportsTruncation(t *testing.T) {
	store := &fakeEnumerator{truncated: true, objects: []storage.ObjectInfo{{Key: "k"}}}
	rec := NewArtifactReconciler(&fakeKeySource{keys: nil}, store, "")
	rep, err := rec.RunOnce(time.Now())
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if !rep.Truncated {
		t.Fatalf("truncated listing must be surfaced in the report")
	}
}

func TestReconciler_PropagatesStoreError(t *testing.T) {
	store := &fakeEnumerator{err: os.ErrPermission}
	rec := NewArtifactReconciler(&fakeKeySource{keys: nil}, store, "")
	if _, err := rec.RunOnce(time.Now()); err == nil {
		t.Fatalf("a listing failure must surface (otherwise it looks like a clean pass)")
	}
}

// interval<=0 = 关闭：不得启动 ticker，也不得先跑一轮（启动路径上扫桶会拖慢就绪）。
func TestReconciler_RunDisabledByZeroInterval(t *testing.T) {
	store := &fakeEnumerator{}
	rec := NewArtifactReconciler(&fakeKeySource{keys: []string{"x"}}, store, "")
	rec.Run(context.Background(), 0)
	if store.gotLimit != 0 {
		t.Fatalf("disabled reconciliation must not touch the store, got a listing call")
	}
}

// 用真实的 Local 驱动跑一遍：验证"列举 → 与 DB 集合比对"这条链在
// 不接对象存储服务的情况下也能端到端工作（无需 MinIO / 集群）。
func TestReconciler_AgainstLocalDriver(t *testing.T) {
	root := t.TempDir()
	store, err := storage.NewLocal(root, "secret", "http://localhost:8080")
	if err != nil {
		t.Fatalf("NewLocal: %v", err)
	}
	write := func(key string) {
		full := filepath.Join(root, filepath.FromSlash(key))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(full, []byte("x"), 0o644); err != nil {
			t.Fatalf("write: %v", err)
		}
	}
	write("components/1/v1/kept.tar")
	write("components/1/v1/orphan.tar")
	write("components/1/v1/inflight.tar.part") // 上传中的半成品：不算对象

	rec := NewArtifactReconciler(&fakeKeySource{keys: []string{
		"components/1/v1/kept.tar",
		"components/1/v1/gone.tar",
	}}, store, "")

	rep, err := rec.RunOnce(time.Now())
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if rep.ObjectKeys != 2 {
		t.Fatalf("the .part upload must not be listed as an object, got %d objects", rep.ObjectKeys)
	}
	if rep.MissingObjects != 1 || rep.OrphanObjects != 1 {
		t.Fatalf("want 1 missing + 1 orphan, got %+v", rep)
	}
	if rep.MissingSample[0] != "components/1/v1/gone.tar" {
		t.Fatalf("missing sample wrong: %v", rep.MissingSample)
	}
	if rep.OrphanSample[0] != "components/1/v1/orphan.tar" {
		t.Fatalf("orphan sample wrong: %v", rep.OrphanSample)
	}
	// 直接断言：对账跑完之后文件都还在（仅报告，不删）。
	for _, key := range []string{"components/1/v1/kept.tar", "components/1/v1/orphan.tar"} {
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(key))); err != nil {
			t.Fatalf("reconciliation must not delete anything, %s is gone: %v", key, err)
		}
	}
}

func TestLocalDriver_ListObjectsHonoursLimitAndReportsTruncation(t *testing.T) {
	root := t.TempDir()
	store, err := storage.NewLocal(root, "secret", "http://localhost:8080")
	if err != nil {
		t.Fatalf("NewLocal: %v", err)
	}
	for _, k := range []string{"a/1", "a/2", "a/3"} {
		full := filepath.Join(root, filepath.FromSlash(k))
		_ = os.MkdirAll(filepath.Dir(full), 0o755)
		if err := os.WriteFile(full, []byte("x"), 0o644); err != nil {
			t.Fatalf("write: %v", err)
		}
	}
	objects, truncated, err := store.ListObjects("a/", 2)
	if err != nil {
		t.Fatalf("ListObjects: %v", err)
	}
	if !truncated || len(objects) != 2 {
		t.Fatalf("limit must bound the listing and set truncated: n=%d truncated=%v", len(objects), truncated)
	}
	if objects[0].Key >= objects[1].Key {
		t.Fatalf("keys must come back sorted: %v", objects)
	}
}

// 前缀不存在（还没上传过任何制品）= 空结果，不是错误。
func TestLocalDriver_ListObjectsMissingPrefixIsEmptyNotError(t *testing.T) {
	store, err := storage.NewLocal(t.TempDir(), "secret", "http://localhost:8080")
	if err != nil {
		t.Fatalf("NewLocal: %v", err)
	}
	objects, truncated, err := store.ListObjects("nope/", 10)
	if err != nil {
		t.Fatalf("a missing prefix must not be an error: %v", err)
	}
	if len(objects) != 0 || truncated {
		t.Fatalf("want empty listing, got %v truncated=%v", objects, truncated)
	}
}
