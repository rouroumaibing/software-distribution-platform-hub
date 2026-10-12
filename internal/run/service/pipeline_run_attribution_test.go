package service

import (
	"testing"

	"github.com/google/uuid"

	"github.com/rouroumaibing/software-distribution-platform-hub/internal/run/models"
)

// run 行反规范化归属（ui-12 §5.1 环境覆盖档 / §6.3 配额 org 维度）：
// 触发时沿 pipeline→component 解析 org、按 (component, target, namespace)
// 唯一命中解析 environment；解析失败/歧义落空值不挡触发（fail-open）。

type fakeComponentMeta struct {
	componentID uuid.UUID
	orgID       uuid.UUID
}

func (f fakeComponentMeta) ResolveComponentOrg(uuid.UUID) (uuid.UUID, uuid.UUID, error) {
	return f.componentID, f.orgID, nil
}

type fakeEnvMeta struct {
	envID uuid.UUID
	ok    bool
}

func (f fakeEnvMeta) UniqueByComponentTargetNamespace(uuid.UUID, uuid.UUID, string) (uuid.UUID, bool) {
	return f.envID, f.ok
}

func TestTriggerStampsOrgAndEnvironmentAttribution(t *testing.T) {
	store := newFakeDispatchStore()
	svc := newFullTestService(&fakeDispatcher{fail: false}, store)
	componentID, orgID, envID := uuid.New(), uuid.New(), uuid.New()
	svc.componentMeta = fakeComponentMeta{componentID: componentID, orgID: orgID}
	svc.environmentMeta = fakeEnvMeta{envID: envID, ok: true}

	targetID := uuid.New()
	runs, err := svc.Trigger(uuid.New(), &models.TriggerRequest{
		TargetID:        &targetID,
		TargetNamespace: "sdp-test",
		TriggeredBy:     "tester",
	})
	if err != nil {
		t.Fatalf("trigger: %v", err)
	}
	run := runs[0]
	if run.OrgID == nil || *run.OrgID != orgID {
		t.Fatalf("run 行应落 org_id，得到 %v", run.OrgID)
	}
	if run.EnvironmentID == nil || *run.EnvironmentID != envID {
		t.Fatalf("run 行应落 environment_id，得到 %v", run.EnvironmentID)
	}
}

func TestTriggerAttribution_AmbiguousEnvStaysEmpty(t *testing.T) {
	// 环境解析歧义/未命中（ok=false）→ environment_id 留空，org_id 照常落；
	// 触发本身不受影响（归属是统计/指派精度问题，不是闸门）。
	store := newFakeDispatchStore()
	svc := newFullTestService(&fakeDispatcher{fail: false}, store)
	orgID := uuid.New()
	svc.componentMeta = fakeComponentMeta{componentID: uuid.New(), orgID: orgID}
	svc.environmentMeta = fakeEnvMeta{envID: uuid.New(), ok: false}

	targetID := uuid.New()
	runs, err := svc.Trigger(uuid.New(), &models.TriggerRequest{
		TargetID:    &targetID,
		TriggeredBy: "tester",
	})
	if err != nil {
		t.Fatalf("trigger: %v", err)
	}
	run := runs[0]
	if run.OrgID == nil || *run.OrgID != orgID {
		t.Fatalf("org_id 应照常落列，得到 %v", run.OrgID)
	}
	if run.EnvironmentID != nil {
		t.Fatalf("歧义解析应留空 environment_id，得到 %s", run.EnvironmentID)
	}
}

func TestTriggerAttribution_AbsentMetaStaysEmpty(t *testing.T) {
	// 未装配 resolver（旧库/单测缺省）→ 两列留空，触发照常。
	store := newFakeDispatchStore()
	svc := newFullTestService(&fakeDispatcher{fail: false}, store)

	targetID := uuid.New()
	runs, err := svc.Trigger(uuid.New(), &models.TriggerRequest{
		TargetID:    &targetID,
		TriggeredBy: "tester",
	})
	if err != nil {
		t.Fatalf("trigger: %v", err)
	}
	if runs[0].OrgID != nil || runs[0].EnvironmentID != nil {
		t.Fatalf("未装配 resolver 应两列留空，得到 org=%v env=%v", runs[0].OrgID, runs[0].EnvironmentID)
	}
}
