// Package cronexpr parses standard 5-field cron expressions (分 时 日 月 周)
// and computes next fire times. Hand-rolled per the S-3 ruling (steelman
// 2026-10-11): zero new dependencies, parser + exhaustive unit tests, capped
// scan so unreachable expressions fail loudly instead of looping forever.
//
// Supported syntax per field:
//
//   - every value
//     */n         every n-th value starting at the field minimum
//     a           single value
//     a-b         inclusive range
//     a-b/n       range with step
//     a,b,c       lists of any of the above
//
// Day-of-month and day-of-week follow standard cron semantics: when BOTH are
// restricted (neither is *), a day matches if EITHER field matches.
package cronexpr

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

const (
	minMinute = 0
	maxMinute = 59
	minHour   = 0
	maxHour   = 23
	minDom    = 1
	maxDom    = 31
	minMonth  = 1
	maxMonth  = 12
	minDow    = 0
	maxDow    = 6 // 0=Sunday … 6=Saturday
)

// maxScanDays bounds the minute-by-minute search for the next fire time.
// Two years is generous for any sane schedule (Feb 29 needs ≤ 4 years for a
// Feb-29-only expression, hence 4+ — use 8 years to be safe).
const maxScanDays = 8 * 366

// Expr is a parsed cron expression; all fields are precomputed bitsets.
type Expr struct {
	raw     string
	minute  []bool // 60
	hour    []bool // 24
	dom     []bool // 32 (index 1..31)
	month   []bool // 13 (index 1..12)
	dow     []bool // 7  (0=Sun)
	domStar bool   // dom field was "*"
	dowStar bool   // dow field was "*"
}

// Parse parses and validates a 5-field cron expression.
func Parse(expr string) (*Expr, error) {
	fields := strings.Fields(strings.TrimSpace(expr))
	if len(fields) != 5 {
		return nil, fmt.Errorf("cron 表达式须为 5 段（分 时 日 月 周），得到 %d 段", len(fields))
	}
	e := &Expr{raw: expr, minute: make([]bool, 60), hour: make([]bool, 24), dom: make([]bool, 32), month: make([]bool, 13), dow: make([]bool, 7)}
	var err error
	if e.domStar, err = fill(e.dom, fields[2], minDom, maxDom); err != nil {
		return nil, fmt.Errorf("日 字段无效: %w", err)
	}
	if e.dowStar, err = fill(e.dow, fields[4], minDow, maxDow); err != nil {
		return nil, fmt.Errorf("周 字段无效: %w", err)
	}
	if _, err = fill(e.minute, fields[0], minMinute, maxMinute); err != nil {
		return nil, fmt.Errorf("分 字段无效: %w", err)
	}
	if _, err = fill(e.hour, fields[1], minHour, maxHour); err != nil {
		return nil, fmt.Errorf("时 字段无效: %w", err)
	}
	if _, err = fill(e.month, fields[3], minMonth, maxMonth); err != nil {
		return nil, fmt.Errorf("月 字段无效: %w", err)
	}
	return e, nil
}

// String returns the raw expression.
func (e *Expr) String() string { return e.raw }

// fill parses one field into the bitset; returns whether the field was "*".
func fill(bits []bool, field string, min, max int) (bool, error) {
	star := field == "*"
	for _, part := range strings.Split(field, ",") {
		if part == "" {
			return false, fmt.Errorf("空列表项")
		}
		// step applies to "*" or a range
		base, stepStr := part, ""
		step := 1
		if i := strings.Index(part, "/"); i >= 0 {
			base, stepStr = part[:i], part[i+1:]
			n, err := strconv.Atoi(stepStr)
			if err != nil || n < 1 {
				return false, fmt.Errorf("步长 %q 须为正整数", stepStr)
			}
			step = n
		}
		lo, hi := min, max
		switch {
		case base == "*":
			// keep default full range
		case strings.Contains(base, "-"):
			bounds := strings.SplitN(base, "-", 2)
			a, err1 := strconv.Atoi(bounds[0])
			b, err2 := strconv.Atoi(bounds[1])
			if err1 != nil || err2 != nil {
				return false, fmt.Errorf("范围 %q 无效", base)
			}
			lo, hi = a, b
		default:
			a, err := strconv.Atoi(base)
			if err != nil {
				return false, fmt.Errorf("值 %q 无效", base)
			}
			lo, hi = a, a
		}
		if lo < min || hi > max || lo > hi {
			return false, fmt.Errorf("范围 %d-%d 超出 [%d,%d]", lo, hi, min, max)
		}
		for v := lo; v <= hi; v += step {
			bits[v] = true
		}
	}
	return star, nil
}

// Next returns the next fire time strictly after `after`, aligned to whole
// minutes. Returns an error if no matching time exists within maxScanDays.
func (e *Expr) Next(after time.Time) (time.Time, error) {
	t := after.Truncate(time.Minute).Add(time.Minute)
	limit := t.AddDate(0, 0, maxScanDays)
	for t.Before(limit) {
		if !e.month[t.Month()] {
			// skip to the first day of next month
			t = time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, t.Location()).AddDate(0, 1, 0)
			continue
		}
		if !e.dayMatches(t) {
			t = time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location()).AddDate(0, 0, 1)
			continue
		}
		if !e.hour[t.Hour()] {
			t = t.Truncate(time.Hour).Add(time.Hour)
			continue
		}
		if !e.minute[t.Minute()] {
			t = t.Add(time.Minute)
			continue
		}
		return t, nil
	}
	return time.Time{}, fmt.Errorf("cron 表达式 %q 在 %d 天内无匹配时刻", e.raw, maxScanDays)
}

// dayMatches applies standard cron dom/dow semantics.
func (e *Expr) dayMatches(t time.Time) bool {
	domOK := e.dom[t.Day()]
	dowOK := e.dow[int(t.Weekday())]
	switch {
	case e.domStar && e.dowStar:
		return true
	case e.domStar:
		return dowOK
	case e.dowStar:
		return domOK
	default:
		// both restricted: standard cron OR rule
		return domOK || dowOK
	}
}

// NextN returns up to n fire times strictly after `after`.
func (e *Expr) NextN(after time.Time, n int) ([]time.Time, error) {
	out := make([]time.Time, 0, n)
	t := after
	for len(out) < n {
		next, err := e.Next(t)
		if err != nil {
			if len(out) > 0 {
				return out, nil
			}
			return nil, err
		}
		out = append(out, next)
		t = next
	}
	return out, nil
}
