package service

import (
	"errors"
	"testing"

	"github.com/google/uuid"
)

// 覆盖 backlog B-14：配置审计行的 environment_key 快照写入路径。
// envKeySnapshot 不碰 repo，所以这里可以只装配一个 fake 解析器（repo 传 nil）。

type fakeEnvKeys struct {
	key   string
	err   error
	calls []uuid.UUID
}

func (f *fakeEnvKeys) ResolveKey(envID uuid.UUID) (string, error) {
	f.calls = append(f.calls, envID)
	return f.key, f.err
}

// 全局默认配置（environment_id IS NULL）不该有 key，也不该触发一次查询。
func TestEnvKeySnapshot_GlobalConfigHasNoKey(t *testing.T) {
	keys := &fakeEnvKeys{key: "beta"}
	svc := NewComponentConfigService(nil, keys)

	if got := svc.envKeySnapshot(nil); got != "" {
		t.Fatalf("global (nil env) snapshot must be empty, got %q", got)
	}
	if len(keys.calls) != 0 {
		t.Fatalf("a nil environment must not trigger a lookup, got %v", keys.calls)
	}
}

func TestEnvKeySnapshot_ResolvesEnvironmentKey(t *testing.T) {
	envID := uuid.New()
	keys := &fakeEnvKeys{key: "beta"}
	svc := NewComponentConfigService(nil, keys)

	if got := svc.envKeySnapshot(&envID); got != "beta" {
		t.Fatalf("want key %q, got %q", "beta", got)
	}
	if len(keys.calls) != 1 || keys.calls[0] != envID {
		t.Fatalf("want one lookup for %s, got %v", envID, keys.calls)
	}
}

// 解析失败降级为 ""：历史行是"发生过什么"的记录，不是配置写入的正确性门禁，
// 不能因为环境刚被删掉就阻断配置变更。
func TestEnvKeySnapshot_DegradesOnResolutionFailure(t *testing.T) {
	envID := uuid.New()
	svc := NewComponentConfigService(nil, &fakeEnvKeys{err: errors.New("environment gone")})

	if got := svc.envKeySnapshot(&envID); got != "" {
		t.Fatalf("resolution failure must degrade to empty snapshot, got %q", got)
	}
}

func TestEnvKeySnapshot_NilResolver(t *testing.T) {
	envID := uuid.New()
	svc := NewComponentConfigService(nil, nil)

	if got := svc.envKeySnapshot(&envID); got != "" {
		t.Fatalf("nil resolver must degrade to empty snapshot, got %q", got)
	}
}
