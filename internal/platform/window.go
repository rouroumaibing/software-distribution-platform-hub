package platform

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// ChangeWindow is the §5.1 变更窗口配置（平台级特性开关）。
//
// 语义（2026-10-12 用户拍板「特性开关 + 设置处 + 拦截」）：Enabled=true 时，
// **仅允许窗口内的时段触发新运行**——窗口外触发被 409 拦截（前端弹窗预拦截 +
// 后端强校验双保险）。这是运维「变更窗口」的标准含义：把变更限制在可控时段。
type ChangeWindow struct {
	// Enabled is the feature switch. false（默认）= 完全放行，窗口字段仅存档。
	Enabled bool `json:"enabled"`

	// Start/End are "HH:MM" 24h clock bounds, inclusive start / exclusive end.
	// Start > End 表示跨零点窗口（如 22:00–06:00）。
	Start string `json:"start"`
	End   string `json:"end"`

	// Weekdays lists allowed weekdays, 0=周日…6=周六（time.Weekday 数值）。
	// 空 = 每天都允许（只受时段约束）。
	Weekdays []int `json:"weekdays,omitempty"`
}

// Validate checks the clock strings and weekday range.
func (cw ChangeWindow) Validate() error {
	if _, err := parseHHMM(cw.Start); err != nil {
		return fmt.Errorf("开始时间非法: %w", err)
	}
	if _, err := parseHHMM(cw.End); err != nil {
		return fmt.Errorf("结束时间非法: %w", err)
	}
	for _, d := range cw.Weekdays {
		if d < 0 || d > 6 {
			return fmt.Errorf("weekday %d 越界（0=周日…6=周六）", d)
		}
	}
	return nil
}

// InWindow reports whether t falls inside the allowed change window.
//
// 跨零点窗口的星期归属：窗口从「开始日」起算 —— 凌晨段（minute < end）按
// **前一天**的星期判定（22:00–06:00 的 01:00 属于前一天晚上的窗口）。
func InWindow(cw ChangeWindow, t time.Time) bool {
	if !cw.Enabled {
		return true
	}
	start, err := parseHHMM(cw.Start)
	if err != nil {
		return false // 配置损坏 = fail-closed
	}
	end, err := parseHHMM(cw.End)
	if err != nil {
		return false
	}
	minutes := t.Hour()*60 + t.Minute()

	weekdayAllowed := func(d time.Weekday) bool {
		if len(cw.Weekdays) == 0 {
			return true
		}
		for _, w := range cw.Weekdays {
			if int(d) == w {
				return true
			}
		}
		return false
	}

	if start <= end {
		if minutes < start || minutes >= end {
			return false
		}
		return weekdayAllowed(t.Weekday())
	}
	// 跨零点：晚间段（>= start）按今天；凌晨段（< end）按昨天。
	if minutes >= start {
		return weekdayAllowed(t.Weekday())
	}
	if minutes < end {
		return weekdayAllowed((t.Weekday() + 6) % 7) // 前一天
	}
	return false
}

func parseHHMM(s string) (int, error) {
	s = strings.TrimSpace(s)
	parts := strings.Split(s, ":")
	if len(parts) != 2 || len(parts[0]) != 2 || len(parts[1]) != 2 {
		return 0, fmt.Errorf("%q 须为严格两位 HH:MM", s)
	}
	h, err := strconv.Atoi(parts[0])
	if err != nil || h < 0 || h > 23 {
		return 0, fmt.Errorf("小时 %q 越界", parts[0])
	}
	m, err := strconv.Atoi(parts[1])
	if err != nil || m < 0 || m > 59 {
		return 0, fmt.Errorf("分钟 %q 越界", parts[1])
	}
	return h*60 + m, nil
}

// ParseChangeWindow decodes the stored JSON value. Empty value = 关闭的默认档。
func ParseChangeWindow(raw string) ChangeWindow {
	var cw ChangeWindow
	if raw != "" {
		_ = json.Unmarshal([]byte(raw), &cw)
	}
	return cw
}

// EncodeChangeWindow marshals the spec for storage.
func EncodeChangeWindow(cw ChangeWindow) (string, error) {
	if err := cw.Validate(); err != nil {
		return "", err
	}
	b, err := json.Marshal(cw)
	return string(b), err
}
