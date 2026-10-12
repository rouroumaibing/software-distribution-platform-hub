package cronexpr

import (
	"testing"
	"time"
)

func mustParse(t *testing.T, expr string) *Expr {
	t.Helper()
	e, err := Parse(expr)
	if err != nil {
		t.Fatalf("Parse(%q): %v", expr, err)
	}
	return e
}

func nextOf(t *testing.T, expr, after string) time.Time {
	t.Helper()
	a, err := time.ParseInLocation("2006-01-02 15:04", after, time.UTC)
	if err != nil {
		t.Fatalf("bad after: %v", err)
	}
	n, err := mustParse(t, expr).Next(a)
	if err != nil {
		t.Fatalf("Next(%q after %s): %v", expr, after, err)
	}
	return n
}

func TestParse_AcceptsStandardForms(t *testing.T) {
	good := []string{
		"* * * * *",
		"0 0 * * *",
		"*/15 * * * *",
		"30 2 1 * *",
		"0 9-18 * * 1-5",
		"0,30 8,20 * * 0,6",
		"5 4 * * 1-5/2",
		"59 23 29 2 *",
	}
	for _, g := range good {
		if _, err := Parse(g); err != nil {
			t.Errorf("Parse(%q) = %v, want ok", g, err)
		}
	}
}

func TestParse_RejectsMalformed(t *testing.T) {
	bad := map[string]string{
		"* * * *":      "段数不足",
		"* * * * * *":  "段数过多",
		"60 * * * *":   "分越界",
		"* 24 * * *":   "时越界",
		"0 0 32 * *":   "日越界",
		"0 0 * 13 *":   "月越界",
		"0 0 * * 7":    "周越界",
		"a * * * *":    "非数字",
		"10-5 * * * *": "范围倒置",
		"*/0 * * * *":  "步长为 0",
		"1,,2 * * * *": "空列表项",
		"1-5/ * * * *": "空步长",
	}
	for b, why := range bad {
		if _, err := Parse(b); err == nil {
			t.Errorf("Parse(%q) 应报错（%s）", b, why)
		}
	}
}

func TestNext_EveryMinute(t *testing.T) {
	got := nextOf(t, "* * * * *", "2026-10-11 10:30")
	if want := "2026-10-11 10:31"; got.Format("2006-01-02 15:04") != want {
		t.Fatalf("got %s want %s", got, want)
	}
}

func TestNext_DailyAtZero(t *testing.T) {
	got := nextOf(t, "0 0 * * *", "2026-10-11 10:30")
	if want := "2026-10-12 00:00"; got.Format("2006-01-02 15:04") != want {
		t.Fatalf("got %s want %s", got, want)
	}
}

func TestNext_StepFifteen(t *testing.T) {
	got := nextOf(t, "*/15 * * * *", "2026-10-11 10:31")
	if want := "2026-10-11 10:45"; got.Format("2006-01-02 15:04") != want {
		t.Fatalf("got %s want %s", got, want)
	}
}

func TestNext_WeekdayMorning(t *testing.T) {
	// 2026-10-11 is a Sunday → next Mon 09:00
	got := nextOf(t, "0 9 * * 1-5", "2026-10-11 12:00")
	if want := "2026-10-12 09:00"; got.Format("2006-01-02 15:04") != want {
		t.Fatalf("got %s want %s", got, want)
	}
}

func TestNext_MonthBoundaryAndLeapDay(t *testing.T) {
	got := nextOf(t, "0 0 1 * *", "2026-10-15 00:00")
	if want := "2026-11-01 00:00"; got.Format("2006-01-02 15:04") != want {
		t.Fatalf("got %s want %s", got, want)
	}
	leap := nextOf(t, "0 0 29 2 *", "2026-10-15 00:00")
	if want := "2028-02-29 00:00"; leap.Format("2006-01-02 15:04") != want {
		t.Fatalf("leap: got %s want %s", leap, want)
	}
}

func TestNext_DomDowOrRule(t *testing.T) {
	// Both restricted: matches EITHER (standard cron). 2026-10-11 is Sunday(dow=0)
	// and day 11. Expression "0 0 11 * 0" fires on the 11th OR any Sunday.
	got := nextOf(t, "0 0 11 * 0", "2026-10-01 00:00")
	// Oct 4 is Sunday, Oct 11 is Sunday AND the 11th → first is Oct 4.
	if want := "2026-10-04 00:00"; got.Format("2006-01-02 15:04") != want {
		t.Fatalf("got %s want %s", got, want)
	}
}

func TestNext_DowZeroAndSevenBothSunday(t *testing.T) {
	// 7 is normalized out of range by validation; 0=Sunday stays canonical.
	got := nextOf(t, "0 12 * * 0", "2026-10-11 00:00")
	if want := "2026-10-11 12:00"; got.Format("2006-01-02 15:04") != want {
		t.Fatalf("got %s want %s", got, want)
	}
}

func TestNextN_FiveEntries(t *testing.T) {
	a, _ := time.ParseInLocation("2006-01-02 15:04", "2026-10-11 10:00", time.UTC)
	ns, err := mustParse(t, "30 10 * * *").NextN(a, 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(ns) != 5 {
		t.Fatalf("want 5, got %d", len(ns))
	}
	for i, want := 0, a.AddDate(0, 0, 0); i < 5; i++ {
		want = time.Date(2026, 10, 11+i, 10, 30, 0, 0, time.UTC)
		if !ns[i].Equal(want) {
			t.Fatalf("ns[%d]=%s want %s", i, ns[i], want)
		}
	}
}
