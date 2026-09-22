package repository

import (
	"strings"
	"testing"
	"time"

	"gorm.io/gorm"

	"github.com/rouroumaibing/software-distribution-platform-hub/internal/permission/models"
)

// The reclaim job (§7.4) must delete ONLY rows whose expires_at is non-NULL and
// in the past. A missing `expires_at IS NOT NULL` would delete permanent grants
// (`NULL < x` is unknown, so it is technically safe, but pinning the predicate
// documents intent); a missing `expires_at <` would wipe live grants. Neither is
// visible from a build, so the emitted SQL is pinned here.
//
// Delete is rendered via ToSQL rather than a DryRun Execute — in this GORM
// build the DryRun flag is not honoured on the Delete path, but ToSQL forces a
// dry render deterministically. The predicate under test is the shared
// expiredScope helper, so a production change to it is caught here.

func TestDeleteExpired_ComponentOnlyExpired(t *testing.T) {
	_, gdb := openDryRun(t)
	sql := gdb.ToSQL(func(tx *gorm.DB) *gorm.DB {
		return expiredScope(tx, time.Now()).Delete(&models.ComponentRoleBinding{})
	})
	t.Logf("SQL: %s", sql)

	if !strings.Contains(sql, "DELETE") || !strings.Contains(sql, "component_role_bindings") {
		t.Fatalf("应当是针对 component_role_bindings 的物理删除:\n%s", sql)
	}
	if !strings.Contains(sql, "expires_at IS NOT NULL") || !strings.Contains(sql, "expires_at <") {
		t.Fatalf("回收谓词必须只命中已过期行:\n%s", sql)
	}
}

func TestDeleteExpired_PlatformOnlyExpired(t *testing.T) {
	_, gdb := openDryRun(t)
	sql := gdb.ToSQL(func(tx *gorm.DB) *gorm.DB {
		return expiredScope(tx, time.Now()).Delete(&models.PlatformRoleBinding{})
	})
	t.Logf("SQL: %s", sql)

	if !strings.Contains(sql, "DELETE") || !strings.Contains(sql, "platform_role_bindings") {
		t.Fatalf("应当是针对 platform_role_bindings 的物理删除:\n%s", sql)
	}
	if !strings.Contains(sql, "expires_at IS NOT NULL") || !strings.Contains(sql, "expires_at <") {
		t.Fatalf("回收谓词必须只命中已过期行:\n%s", sql)
	}
}
