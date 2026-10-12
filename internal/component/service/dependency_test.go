package service

import (
	"testing"

	"github.com/google/uuid"

	"github.com/rouroumaibing/software-distribution-platform-hub/internal/component/models"
)

func edge(from, to string) models.ComponentDependency {
	return models.ComponentDependency{ComponentID: uuid.MustParse(from), DependsOnID: uuid.MustParse(to)}
}

var (
	idA = "00000000-0000-0000-0000-00000000000a"
	idB = "00000000-0000-0000-0000-00000000000b"
	idC = "00000000-0000-0000-0000-00000000000c"
	idD = "00000000-0000-0000-0000-00000000000d"
)

func TestClosesCycle_Detected(t *testing.T) {
	// A→B→C 已存在；加 C→A 应判环。
	all := []models.ComponentDependency{edge(idA, idB), edge(idB, idC)}
	if !closesCycle(all, uuid.MustParse(idC), uuid.MustParse(idA)) {
		t.Fatal("C→A 应关闭 A→B→C→A 环")
	}
}

func TestClosesCycle_AcyclicAllowed(t *testing.T) {
	// A→B→C 已存在；加 D→A（新上游）与 A→D（新下游）都不成环。
	all := []models.ComponentDependency{edge(idA, idB), edge(idB, idC)}
	if closesCycle(all, uuid.MustParse(idD), uuid.MustParse(idA)) {
		t.Fatal("D→A 不成环")
	}
	if closesCycle(all, uuid.MustParse(idA), uuid.MustParse(idD)) {
		t.Fatal("A→D 不成环")
	}
	// 已有 A→B；再加 B→A 成环。
	all2 := []models.ComponentDependency{edge(idA, idB)}
	if !closesCycle(all2, uuid.MustParse(idB), uuid.MustParse(idA)) {
		t.Fatal("B→A 应关闭双向环")
	}
}

func TestValidLifecycle(t *testing.T) {
	for _, ok := range []string{"active", "frozen", "deprecated"} {
		if !models.ValidLifecycle(ok) {
			t.Errorf("%q 应合法", ok)
		}
	}
	for _, bad := range []string{"", "Active", "archived", "deleted"} {
		if models.ValidLifecycle(bad) {
			t.Errorf("%q 应非法", bad)
		}
	}
}
