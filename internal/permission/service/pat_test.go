package service

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/rouroumaibing/software-distribution-platform-hub/internal/permission/models"
)

type patFakeRepo struct {
	rows  map[string]*models.PersonalAccessToken // by hash
	nowFn func() time.Time
}

func (f *patFakeRepo) Create(t *models.PersonalAccessToken) error {
	f.rows[t.TokenHash] = t
	return nil
}
func (f *patFakeRepo) FindByHash(hash string) (*models.PersonalAccessToken, error) {
	if r, ok := f.rows[hash]; ok {
		return r, nil
	}
	return nil, ErrPATNotFound
}
func (f *patFakeRepo) ListBySubject(string) ([]models.PersonalAccessToken, error) { return nil, nil }
func (f *patFakeRepo) Revoke(_ string, _ uuid.UUID, _ time.Time) error            { return nil }
func (f *patFakeRepo) TouchLastUsed(uuid.UUID, time.Time)                         {}

func newPATTestService(nowFn func() time.Time) (*PATService, *patFakeRepo) {
	repo := &patFakeRepo{rows: map[string]*models.PersonalAccessToken{}}
	return &PATService{repo: repo, now: nowFn}, repo
}

func TestPAT_CreateAndVerify(t *testing.T) {
	svc, _ := newPATTestService(time.Now)
	row, plaintext, err := svc.Create("user-1", CreateTokenInput{Name: "ci"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if !strings.HasPrefix(plaintext, "sdp_pat_") || len(plaintext) != len("sdp_pat_")+32 {
		t.Fatalf("明文格式不符: %s", plaintext)
	}
	if row.TokenHash == plaintext || row.TokenHash == "" {
		t.Fatal("库里只存哈希且非空")
	}
	got, err := svc.Verify(plaintext)
	if err != nil || got.Subject != "user-1" {
		t.Fatalf("verify: %v / %+v", err, got)
	}
	if _, err := svc.Verify("sdp_pat_" + strings.Repeat("0", 32)); err == nil {
		t.Fatal("未知 token 应 fail-closed")
	}
}

func TestPAT_Rejected(t *testing.T) {
	now := time.Now()
	svc, repo := newPATTestService(func() time.Time { return now })

	// ① 非法 scope
	if _, _, err := svc.Create("u", CreateTokenInput{Name: "x", Scopes: "admin"}); err == nil {
		t.Fatal("admin scope 应拒绝")
	}
	// ② 过期
	row, plaintext, err := svc.Create("u", CreateTokenInput{Name: "exp"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	past := now.Add(-time.Hour)
	repo.rows[row.TokenHash].ExpiresAt = &past
	if _, err := svc.Verify(plaintext); err == nil {
		t.Fatal("过期 token 应 fail-closed")
	}
	// ③ 吊销
	row2, plaintext2, err := svc.Create("u", CreateTokenInput{Name: "rev"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	repo.rows[row2.TokenHash].RevokedAt = &now
	if _, err := svc.Verify(plaintext2); err == nil {
		t.Fatal("已吊销 token 应 fail-closed")
	}
}

func TestPAT_UniqueHashes(t *testing.T) {
	svc, repo := newPATTestService(time.Now)
	_, p1, _ := svc.Create("u", CreateTokenInput{Name: "a"})
	_, p2, _ := svc.Create("u", CreateTokenInput{Name: "b"})
	if p1 == p2 {
		t.Fatal("两次生成不应撞明文")
	}
	if len(repo.rows) != 2 {
		t.Fatalf("应有两行独立哈希，实际 %d", len(repo.rows))
	}
}
