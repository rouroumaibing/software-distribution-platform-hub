package service

import (
	"errors"
	"testing"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	"github.com/rouroumaibing/software-distribution-platform-hub/internal/target/repository"
)

// 测试策略与仓库既有 dryrun 惯例一致：gorm Open 带 DryRun + DisableAutomaticPing
// （不实际建库），验证服务层守卫与错误映射；原子消费的 SQL 语义（条件 UPDATE、
// 单次生效、过期判定）由 BurnEnrollToken 的 WHERE 构造保证并随真集群 E2E 回归。

func newTestTargetService(t *testing.T) *TargetService {
	t.Helper()
	gdb, err := gorm.Open(postgres.New(postgres.Config{DSN: "host=127.0.0.1 user=x dbname=x"}), &gorm.Config{
		DryRun:               true,
		DisableAutomaticPing: true,
	})
	if err != nil {
		t.Fatalf("open dry-run gorm: %v", err)
	}
	return NewTargetService(repository.NewTargetRepository(gdb))
}

func TestConsumeEnrollTokenEmptyToken(t *testing.T) {
	svc := newTestTargetService(t)
	if _, _, err := svc.ConsumeEnrollToken("", "tg-a"); !errors.Is(err, ErrEnrollTokenInvalid) {
		t.Fatalf("empty token: want ErrEnrollTokenInvalid, got %v", err)
	}
}

func TestConsumeEnrollTokenUnknownOrUsedToken(t *testing.T) {
	// dry-run 下 UPDATE 无命中（RowsAffected==0）⇒ token 未知 / 已消费 /
	// 过期 / 名字断言不匹配统一走该路径：确认映射为 ErrEnrollTokenInvalid
	// （gateway 据此只回 401，不泄漏失败原因）。
	svc := newTestTargetService(t)
	if _, _, err := svc.ConsumeEnrollToken("some-token", "tg-a"); !errors.Is(err, ErrEnrollTokenInvalid) {
		t.Fatalf("unconsumable token: want ErrEnrollTokenInvalid, got %v", err)
	}
}

func TestGetByCredentialEmptyRejected(t *testing.T) {
	svc := newTestTargetService(t)
	if _, err := svc.GetByCredential(""); !errors.Is(err, ErrEnrollTokenInvalid) {
		t.Fatalf("empty credential: want ErrEnrollTokenInvalid, got %v", err)
	}
}

func TestNewRunnerCredentialShape(t *testing.T) {
	a, err := newRunnerCredential()
	if err != nil {
		t.Fatalf("mint: %v", err)
	}
	b, err := newRunnerCredential()
	if err != nil {
		t.Fatalf("mint: %v", err)
	}
	if len(a) != 64 || len(b) != 64 {
		t.Fatalf("credential length = %d/%d, want 64 hex chars", len(a), len(b))
	}
	if a == b {
		t.Fatal("two minted credentials identical")
	}
}
