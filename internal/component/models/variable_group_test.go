package models

import (
	"testing"

	"gorm.io/datatypes"
)

func jsonEntries(pairs ...[2]string) datatypes.JSON {
	s := "["
	for i, p := range pairs {
		if i > 0 {
			s += ","
		}
		s += `{"key":"` + p[0] + `","value":"` + p[1] + `"}`
	}
	return datatypes.JSON(s + "]")
}

// 组拍平：同名 key 由「后应用的组」覆盖（resolver 按优先级升序依次应用）；
// 空 key 跳过；坏 JSON 体降级为空组，不 panic。
func TestGroupVariableParams(t *testing.T) {
	lower := VariableGroup{Name: "base", Variables: jsonEntries([2]string{"LOG_LEVEL", "info"}, [2]string{"REGION", "cn-north"})}
	higher := VariableGroup{Name: "team", Variables: jsonEntries([2]string{"LOG_LEVEL", "debug"}, [2]string{"", "skipped"})}
	broken := VariableGroup{Name: "bad", Variables: datatypes.JSON("not-json")}

	out := GroupVariableParams([]VariableGroup{lower, higher, broken})
	got := map[string]string{}
	for _, e := range out {
		got[e.Name] = e.Value
	}
	if got["LOG_LEVEL"] != "debug" {
		t.Errorf("later group must win duplicate key, got %q", got["LOG_LEVEL"])
	}
	if got["REGION"] != "cn-north" {
		t.Errorf("REGION = %q, want cn-north", got["REGION"])
	}
	if _, ok := got[""]; ok {
		t.Errorf("empty key must be skipped")
	}
	if len(out) != 2 {
		t.Errorf("out = %d entries, want 2", len(out))
	}
}
