package service

import (
	"testing"

	"github.com/google/uuid"
	runnerapi "github.com/rouroumaibing/software-distribution-platform-runner/api/v1alpha1"

	"gorm.io/datatypes"

	pipelinemodels "github.com/rouroumaibing/software-distribution-platform-hub/internal/pipeline/models"
)

func param(name, value string) runnerapi.Param { return runnerapi.Param{Name: name, Value: value} }

// N-26 触发参数解析：声明默认 < 变量组 < 组件变量 < 运行输入，逐层钉死。
// 这条链是「触发弹窗与运行时行为一致」的根基（§5.3 验收 gate），任何一层
// 静默反转会直接造成"界面上填的值和任务里跑的值不一样"。
func TestTriggerParamsResolutionOrder(t *testing.T) {
	p := &pipelinemodels.Pipeline{
		Params: datatypes.JSON(`[
			{"name":"IMAGE","type":"string","default":"app:default"},
			{"name":"REPLICAS","type":"number","default":"3"},
			{"name":"REGION","type":"string"}
		]`),
	}
	svc := &PipelineRunService{}
	if svc.varSource != nil {
		t.Fatalf("fresh service must have nil varSource")
	}

	runParams := []runnerapi.Param{param("IMAGE", "app:v9"), param("EXTRA", "1")}
	got := svc.triggerParams(p, runParams)
	m := map[string]string{}
	for _, prm := range got {
		m[prm.Name] = prm.Value
	}
	// 运行输入覆盖声明默认
	if m["IMAGE"] != "app:v9" {
		t.Errorf("IMAGE = %q, want app:v9 (run input wins)", m["IMAGE"])
	}
	// 无运行输入时用声明默认
	if m["REPLICAS"] != "3" {
		t.Errorf("REPLICAS = %q, want 3 (declared default)", m["REPLICAS"])
	}
	// 无默认的声明只出现在有值时
	if v, ok := m["REGION"]; ok && v != "" {
		t.Errorf("REGION should be absent without default/source, got %q", v)
	}
	// 运行输入直通
	if m["EXTRA"] != "1" {
		t.Errorf("EXTRA = %q, want 1", m["EXTRA"])
	}
}

// varSource 提供：组件变量覆盖组变量；运行输入覆盖两者。
func TestTriggerParamsWithVarSource(t *testing.T) {
	p := &pipelinemodels.Pipeline{Params: datatypes.JSON(`[{"name":"A","default":"decl"}]`)}
	svc := &PipelineRunService{}
	svc.SetVarSource(fakeVarSource{
		group:     []runnerapi.Param{param("A", "from-group"), param("G_ONLY", "g")},
		component: []runnerapi.Param{param("A", "from-component"), param("C_ONLY", "c")},
	})

	got := svc.triggerParams(p, []runnerapi.Param{param("A", "from-run")})
	m := map[string]string{}
	for _, prm := range got {
		m[prm.Name] = prm.Value
	}
	if m["A"] != "from-run" {
		t.Errorf("A = %q, want from-run (run input wins)", m["A"])
	}
	if m["G_ONLY"] != "g" || m["C_ONLY"] != "c" {
		t.Errorf("tiers missing: %+v", m)
	}
}

// 组件变量 > 组变量的冲突方向。
func TestTriggerParamsComponentBeatsGroup(t *testing.T) {
	p := &pipelinemodels.Pipeline{}
	svc := &PipelineRunService{}
	svc.SetVarSource(fakeVarSource{
		group:     []runnerapi.Param{param("K", "group")},
		component: []runnerapi.Param{param("K", "component")},
	})
	got := svc.triggerParams(p, nil)
	if len(got) != 1 || got[0].Value != "component" {
		t.Fatalf("component var must beat group var, got %+v", got)
	}
}

type fakeVarSource struct {
	group     []runnerapi.Param
	component []runnerapi.Param
}

func (f fakeVarSource) ComponentParams(uuid.UUID) []runnerapi.Param { return f.component }
func (f fakeVarSource) GroupParams(uuid.UUID) []runnerapi.Param     { return f.group }
