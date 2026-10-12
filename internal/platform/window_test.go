package platform

import (
	"testing"
	"time"
)

func ts(y int, mo time.Month, d, h, mi int) time.Time {
	return time.Date(y, mo, d, h, mi, 0, 0, time.UTC)
}

func TestInWindow_DisabledAlwaysTrue(t *testing.T) {
	cw := ChangeWindow{Enabled: false, Start: "00:00", End: "00:01"}
	if !InWindow(cw, ts(2026, 10, 12, 3, 0)) {
		t.Fatal("开关关闭 = 完全放行")
	}
}

func TestInWindow_BasicDaily(t *testing.T) {
	cw := ChangeWindow{Enabled: true, Start: "09:00", End: "18:00"}
	cases := []struct {
		at   time.Time
		want bool
	}{
		{ts(2026, 10, 12, 9, 0), true},   // 起点 inclusive
		{ts(2026, 10, 12, 12, 30), true}, // 中间
		{ts(2026, 10, 12, 17, 59), true},
		{ts(2026, 10, 12, 18, 0), false}, // 终点 exclusive
		{ts(2026, 10, 12, 6, 0), false},
	}
	for _, c := range cases {
		if got := InWindow(cw, c.at); got != c.want {
			t.Errorf("InWindow(%s) = %v, want %v", c.at, got, c.want)
		}
	}
}

func TestInWindow_Overnight(t *testing.T) {
	cw := ChangeWindow{Enabled: true, Start: "22:00", End: "06:00"}
	cases := []struct {
		at   time.Time
		want bool
	}{
		{ts(2026, 10, 12, 22, 0), true},  // 晚段起点
		{ts(2026, 10, 12, 23, 59), true}, // 晚段
		{ts(2026, 10, 13, 0, 0), true},   // 跨零点
		{ts(2026, 10, 13, 5, 59), true},  // 凌晨段终点前
		{ts(2026, 10, 13, 6, 0), false},  // 窗口闭合
		{ts(2026, 10, 13, 12, 0), false}, // 白天
	}
	for _, c := range cases {
		if got := InWindow(cw, c.at); got != c.want {
			t.Errorf("InWindow(%s) = %v, want %v", c.at, got, c.want)
		}
	}
}

func TestInWindow_Weekdays(t *testing.T) {
	// 2026-10-12 是周一；10-11 周日。
	cw := ChangeWindow{Enabled: true, Start: "00:00", End: "23:59", Weekdays: []int{1, 2, 3, 4, 5}}
	if !InWindow(cw, ts(2026, 10, 12, 10, 0)) {
		t.Fatal("周一应允许")
	}
	if InWindow(cw, ts(2026, 10, 11, 10, 0)) {
		t.Fatal("周日应拒绝")
	}
	// 跨零点凌晨段按「前一天」：周日 01:00 属于周六晚间的窗口 → 周六(6)不在集 → 拒；
	// 周一 01:00 属于周日晚间 → 拒；若窗口含周日(0)则周一凌晨放行。
	overnight := ChangeWindow{Enabled: true, Start: "22:00", End: "06:00", Weekdays: []int{0}}
	if !InWindow(overnight, ts(2026, 10, 12, 1, 0)) { // 周一凌晨 ← 周日晚间窗口
		t.Fatal("周一 01:00 应按周日晚间窗口（周日∈集）放行")
	}
	if InWindow(overnight, ts(2026, 10, 13, 1, 0)) { // 周二凌晨 ← 周一晚间（周一∉集）
		t.Fatal("周二 01:00 应按周一晚间窗口（周一∉集）拒绝")
	}
}

func TestInWindow_BrokenConfigFailsClosed(t *testing.T) {
	cw := ChangeWindow{Enabled: true, Start: "bad", End: "18:00"}
	if InWindow(cw, ts(2026, 10, 12, 10, 0)) {
		t.Fatal("配置损坏应 fail-closed")
	}
}

func TestChangeWindow_Roundtrip(t *testing.T) {
	cw := ChangeWindow{Enabled: true, Start: "22:00", End: "06:00", Weekdays: []int{1, 5}}
	raw, err := EncodeChangeWindow(cw)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	got := ParseChangeWindow(raw)
	if got.Enabled != cw.Enabled || got.Start != cw.Start || got.End != cw.End ||
		len(got.Weekdays) != len(cw.Weekdays) {
		t.Errorf("roundtrip 不一致: %+v vs %+v", got, cw)
	}
	for i := range cw.Weekdays {
		if got.Weekdays[i] != cw.Weekdays[i] {
			t.Errorf("weekdays[%d] 不一致", i)
		}
	}
	if _, err := EncodeChangeWindow(ChangeWindow{Enabled: true, Start: "9:0", End: "18:00"}); err == nil {
		t.Fatal("非法 HH:MM 应被 Validate 拒绝")
	}
	if ParseChangeWindow("").Enabled {
		t.Fatal("空值 = 默认关闭")
	}
}
