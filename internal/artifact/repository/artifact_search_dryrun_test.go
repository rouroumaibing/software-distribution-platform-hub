package repository

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/rouroumaibing/software-distribution-platform-hub/internal/artifact/models"
	"github.com/rouroumaibing/software-distribution-platform-hub/internal/common"
)

// sqlRecorder captures DryRun-generated SQL (same手法 as permission repo dryrun).
type sqlRecorder struct{ parts []string }

func (r *sqlRecorder) Printf(_ string, format string, args ...any) {
	r.parts = append(r.parts, strings.TrimRight(fmt.Sprintf(format, args...), "\n"))
}
func (r *sqlRecorder) joined() string { return strings.Join(r.parts, "\n") }

// logger.Interface 全量实现（DryRun 只走 Trace 里的 fc）。
func (r *sqlRecorder) LogMode(logger.LogLevel) logger.Interface { return r }
func (r *sqlRecorder) Info(context.Context, string, ...any)     {}
func (r *sqlRecorder) Warn(context.Context, string, ...any)     {}
func (r *sqlRecorder) Error(context.Context, string, ...any)    {}
func (r *sqlRecorder) Trace(_ context.Context, _ time.Time, fc func() (string, int64), _ error) {
	sql, _ := fc()
	if sql != "" {
		r.parts = append(r.parts, sql)
	}
}

func openArtifactDryRun(t *testing.T) (*sqlRecorder, *gorm.DB) {
	t.Helper()
	rec := &sqlRecorder{}
	gdb, err := gorm.Open(postgres.New(postgres.Config{DSN: "host=127.0.0.1 user=x dbname=x"}), &gorm.Config{
		DryRun:               true,
		DisableAutomaticPing: true,
		Logger:               rec,
	})
	if err != nil {
		t.Fatalf("dry-run gorm open: %v", err)
	}
	return rec, gdb.Session(&gorm.Session{DryRun: true})
}

var _ = logger.Info

func TestArtifactSearch_Shape(t *testing.T) {
	rec, gdb := openArtifactDryRun(t)
	repo := NewArtifactRepository(gdb)

	since := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	until := since.Add(72 * time.Hour)
	signed := true
	cid := uuid.New()

	_, _, err := repo.Search(ArtifactSearchFilter{
		Version: "1.2", ComponentID: cid, ArtifactType: "image",
		Since: &since, Until: &until, ScanStatus: models.ScanScanned, Signed: &signed,
	}, common.Pagination{Page: 1, PageSize: 50})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	sql := rec.joined()
	for _, want := range []string{
		"cleanup_state <>", // live 谓词必须存在（排除 GC 待删行）
		"version ILIKE",    // 版本子串
		"component_id =",   // 组件限定
		"artifact_type =",
		"created_at >=",
		"created_at <=",
		"scan_status =",
		"signed =",
	} {
		if !strings.Contains(sql, want) {
			t.Errorf("SQL 缺谓词 %q:\n%s", want, sql)
		}
	}
	// 空过滤器：只有 live 谓词，不应夹带任何过滤列。
	rec2, gdb2 := openArtifactDryRun(t)
	repo2 := NewArtifactRepository(gdb2)
	if _, _, err := repo2.Search(ArtifactSearchFilter{}, common.Pagination{Page: 1, PageSize: 50}); err != nil {
		t.Fatalf("empty search: %v", err)
	}
	sql2 := rec2.joined()
	if !strings.Contains(sql2, "cleanup_state <>") {
		t.Errorf("空过滤器也应带 live 谓词:\n%s", sql2)
	}
	for _, unwanted := range []string{"version ILIKE", "scan_status =", "signed ="} {
		if strings.Contains(sql2, unwanted) {
			t.Errorf("空过滤器不应带 %q:\n%s", unwanted, sql2)
		}
	}
}
