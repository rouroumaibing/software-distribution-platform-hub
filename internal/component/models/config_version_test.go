package models

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func mkCfgRow(key, value string, secret bool) ComponentConfig {
	return ComponentConfig{Key: key, Value: value, IsSecret: secret}
}

// 快照体 = 全量 key 集（排序稳定）+ 密钥掩码 —— §4.1 的"整环境快照"语义，
// 且掩码必须在这里发生：history/快照/diff 都不允许出现明文密钥。
func TestBuildConfigSnapshotMasksAndSorts(t *testing.T) {
	rows := []ComponentConfig{
		mkCfgRow("replicas", "3", false),
		mkCfgRow("db_password", "hunter2", true),
		mkCfgRow("image", "app:v1", false),
	}
	entries := BuildConfigSnapshot(rows)
	if len(entries) != 3 {
		t.Fatalf("entries = %d, want 3", len(entries))
	}
	if entries[0].Key != "db_password" || entries[1].Key != "image" || entries[2].Key != "replicas" {
		t.Fatalf("entries must be key-sorted, got %s,%s,%s", entries[0].Key, entries[1].Key, entries[2].Key)
	}
	for _, e := range entries {
		if e.Key == "db_password" {
			if e.Value != "<secret>" {
				t.Errorf("secret value must be masked, got %q", e.Value)
			}
			if !e.IsSecret {
				t.Errorf("secret flag lost")
			}
		}
	}
}

func TestConfigSnapshotJSONRoundTrip(t *testing.T) {
	entries := BuildConfigSnapshot([]ComponentConfig{
		mkCfgRow("a", "1", false),
		mkCfgRow("b", "", true),
	})
	body, err := EncodeConfigSnapshot(entries)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	back, err := DecodeConfigSnapshot(body)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(back) != 2 || back[0].Key != "a" || back[1].Key != "b" || back[1].Value != "<secret>" {
		t.Fatalf("round-trip mismatch: %+v", back)
	}
}

// diff 语义：added / removed / modified 逐类钉住；identical 判定、排序稳定性、
// 以及"两个 <secret> 永不比较密文"的掩码纪律。
func TestDiffConfigSnapshots(t *testing.T) {
	at := time.Date(2026, 10, 11, 0, 0, 0, 0, time.UTC)
	from := []ConfigSnapshotEntry{
		{Key: "image", Value: "app:v1"},
		{Key: "replicas", Value: "3"},
		{Key: "old_key", Value: "x"},
		{Key: "db_password", Value: "<secret>", IsSecret: true, SecretRef: "cred-1"},
	}
	to := []ConfigSnapshotEntry{
		{Key: "image", Value: "app:v2"},
		{Key: "replicas", Value: "3"},
		{Key: "new_key", Value: "y"},
		{Key: "db_password", Value: "<secret>", IsSecret: true, SecretRef: "cred-2"},
	}
	d := DiffConfigSnapshots(from, to, 3, 4, at, at)
	if d.Identical {
		t.Fatalf("expected non-identical diff")
	}
	if d.Summary.Modified != 2 || d.Summary.Added != 1 || d.Summary.Removed != 1 {
		t.Fatalf("summary = %+v, want modified=2 added=1 removed=1", d.Summary)
	}
	byKey := map[string]ConfigKeyDiff{}
	for _, k := range d.Keys {
		byKey[k.Key] = k
	}
	if k := byKey["image"]; k.Change != ConfigChangeModified || k.FromValue != "app:v1" || k.ToValue != "app:v2" {
		t.Errorf("image diff = %+v", k)
	}
	if k := byKey["new_key"]; k.Change != ConfigChangeAdded {
		t.Errorf("new_key diff = %+v", k)
	}
	if k := byKey["old_key"]; k.Change != ConfigChangeRemoved {
		t.Errorf("old_key diff = %+v", k)
	}
	// 密钥：值恒为掩码，变化通过 SecretRef 体现（modified），明文永不出现。
	if k := byKey["db_password"]; k.Change != ConfigChangeModified || k.FromValue != "cred-1" || k.ToValue != "cred-2" {
		t.Errorf("secret diff = %+v", k)
	}
}

func TestDiffConfigSnapshotsIdentical(t *testing.T) {
	at := time.Date(2026, 10, 11, 0, 0, 0, 0, time.UTC)
	same := []ConfigSnapshotEntry{{Key: "a", Value: "1"}, {Key: "b", Value: "2"}}
	d := DiffConfigSnapshots(same, same, 1, 2, at, at)
	if !d.Identical || len(d.Keys) != 0 {
		t.Fatalf("identical bodies must produce empty diff, got %+v", d)
	}
}

// ScopeKeyOf：全局 scope 是 ""（可与 environment uuid 列区分开，uniqueIndex
// 才能在 NULL environment_id 下生效）。
func TestScopeKeyOf(t *testing.T) {
	if got := ScopeKeyOf(nil); got != "" {
		t.Errorf("global scope key = %q, want \"\"", got)
	}
	id := uuid.New()
	if got := ScopeKeyOf(&id); got == "" || got != id.String() {
		t.Errorf("env scope key = %q, want %s", got, id)
	}
}
