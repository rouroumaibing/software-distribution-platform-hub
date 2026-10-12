package service

import (
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/rouroumaibing/software-distribution-platform-hub/internal/run/models"
)

// ui-12 §5.1 / 决策 #4：审批策略校验、指派播种、审批中心视图。

type fakePolicyStore struct {
	rows map[string]*models.ApprovalPolicy // key: componentID|envID
}

func newFakePolicyStore() *fakePolicyStore {
	return &fakePolicyStore{rows: map[string]*models.ApprovalPolicy{}}
}

func policyKey(componentID, envID uuid.UUID) string {
	return componentID.String() + "|" + envID.String()
}

func (f *fakePolicyStore) UpsertPolicy(p *models.ApprovalPolicy) error {
	envID := uuid.Nil
	if p.EnvironmentID != nil {
		envID = *p.EnvironmentID
	}
	cp := *p
	f.rows[policyKey(p.ComponentID, envID)] = &cp
	return nil
}

func (f *fakePolicyStore) ListPolicies(componentID uuid.UUID) ([]models.ApprovalPolicy, error) {
	var out []models.ApprovalPolicy
	for _, r := range f.rows {
		if r.ComponentID == componentID {
			out = append(out, *r)
		}
	}
	return out, nil
}

func (f *fakePolicyStore) PolicyFor(componentID, environmentID uuid.UUID) (*models.ApprovalPolicy, error) {
	if p, ok := f.rows[policyKey(componentID, environmentID)]; ok {
		cp := *p
		return &cp, nil
	}
	return nil, gormNotFound()
}

func TestUpsertApprovalPolicy_Validation(t *testing.T) {
	store := newFakePolicyStore()
	svc := &PipelineRunService{}
	svc.SetApprovalPolicyStore(store)

	if err := svc.UpsertApprovalPolicy(&models.ApprovalPolicy{ComponentID: uuid.New(), Mode: "sometimes"}, "u"); err == nil {
		t.Fatalf("非法 mode 应被拒")
	}
	if err := svc.UpsertApprovalPolicy(&models.ApprovalPolicy{ComponentID: uuid.New(), Mode: "n", MinApprovals: 0}, "u"); err == nil {
		t.Fatalf("mode=n 且 minApprovals=0 应被拒")
	}
	if err := svc.UpsertApprovalPolicy(&models.ApprovalPolicy{ComponentID: uuid.New(), Mode: "any", Assignees: "[1,2]"}, "u"); err == nil {
		t.Fatalf("assignees 非字符串数组应被拒")
	}
	if err := svc.UpsertApprovalPolicy(&models.ApprovalPolicy{ComponentID: uuid.New(), Mode: "n", MinApprovals: 2, Assignees: `["alice","bob"]`}, "u"); err != nil {
		t.Fatalf("合法策略应通过: %v", err)
	}
}

func TestUpsertApprovalPolicy_RequiresStore(t *testing.T) {
	svc := &PipelineRunService{}
	if err := svc.UpsertApprovalPolicy(&models.ApprovalPolicy{ComponentID: uuid.New(), Mode: "any"}, "u"); err == nil {
		t.Fatalf("未装配策略存储应报错（静默成功会误导前端）")
	}
}

func TestApplyApprovalPolicy_SeedsAssignee(t *testing.T) {
	store := newFakePolicyStore()
	svc := &PipelineRunService{}
	svc.SetApprovalPolicyStore(store)
	componentID := uuid.New()
	_ = svc.UpsertApprovalPolicy(&models.ApprovalPolicy{
		ComponentID: componentID, Mode: "n", MinApprovals: 2, Assignees: `["alice","bob"]`,
	}, "u")

	pa := &models.PipelineApproval{}
	svc.applyApprovalPolicy(componentID, nil, pa)
	if pa.Assignee != `["alice","bob"]` {
		t.Fatalf("审批行应播种指派列表，得到 %q", pa.Assignee)
	}
	// 无策略组件 → 空（任意审批人）
	pa2 := &models.PipelineApproval{}
	svc.applyApprovalPolicy(uuid.New(), nil, pa2)
	if pa2.Assignee != "" {
		t.Fatalf("无策略应回退空指派，得到 %q", pa2.Assignee)
	}
}

func TestApplyApprovalPolicy_EnvOverrideNotUsedWithoutBinding(t *testing.T) {
	// run 未绑定环境（nil）时环境覆盖行不得被误用为默认——否则会静默改变
	// 所有环境的审批行为。绑定解析在触发路径（EnvironmentMetaStore）。
	store := newFakePolicyStore()
	svc := &PipelineRunService{}
	svc.SetApprovalPolicyStore(store)
	componentID := uuid.New()
	envID := uuid.New()
	_ = svc.UpsertApprovalPolicy(&models.ApprovalPolicy{
		ComponentID: componentID, EnvironmentID: &envID, Mode: "any", Assignees: `["env-only"]`,
	}, "u")

	pa := &models.PipelineApproval{}
	svc.applyApprovalPolicy(componentID, nil, pa)
	if pa.Assignee != "" {
		t.Fatalf("无环境绑定时环境覆盖行不应生效，得到 %q", pa.Assignee)
	}
}

func TestApplyApprovalPolicy_EnvOverrideWinsWhenBound(t *testing.T) {
	// run 已绑定环境且存在环境覆盖档 → 覆盖档优先（ui-12 §5.1 两档语义）。
	store := newFakePolicyStore()
	svc := &PipelineRunService{}
	svc.SetApprovalPolicyStore(store)
	componentID := uuid.New()
	envID := uuid.New()
	_ = svc.UpsertApprovalPolicy(&models.ApprovalPolicy{
		ComponentID: componentID, Mode: "any", Assignees: `["default-tier"]`,
	}, "u")
	_ = svc.UpsertApprovalPolicy(&models.ApprovalPolicy{
		ComponentID: componentID, EnvironmentID: &envID, Mode: "any", Assignees: `["env-tier"]`,
	}, "u")

	pa := &models.PipelineApproval{}
	svc.applyApprovalPolicy(componentID, &envID, pa)
	if pa.Assignee != `["env-tier"]` {
		t.Fatalf("绑定环境时应取环境覆盖档，得到 %q", pa.Assignee)
	}
}

func TestApplyApprovalPolicy_EnvBoundFallsBackToDefault(t *testing.T) {
	// 绑定了环境但该环境无覆盖档 → 回退组件默认档（不是空指派）。
	store := newFakePolicyStore()
	svc := &PipelineRunService{}
	svc.SetApprovalPolicyStore(store)
	componentID := uuid.New()
	envID := uuid.New()
	_ = svc.UpsertApprovalPolicy(&models.ApprovalPolicy{
		ComponentID: componentID, Mode: "any", Assignees: `["default-tier"]`,
	}, "u")

	pa := &models.PipelineApproval{}
	svc.applyApprovalPolicy(componentID, &envID, pa)
	if pa.Assignee != `["default-tier"]` {
		t.Fatalf("环境无覆盖档应回退组件默认档，得到 %q", pa.Assignee)
	}
}

func TestApprovalCenter_Validation(t *testing.T) {
	approvals := &stubApprovalStore{}
	svc := &PipelineRunService{approvalRepo: approvals}

	if _, err := svc.ApprovalCenter(ListCenterView{View: "all", Subject: "u"}); err == nil || !strings.Contains(err.Error(), "view") {
		t.Fatalf("非法 view 应被拒: %v", err)
	}
	if _, err := svc.ApprovalCenter(ListCenterView{View: "pending", Subject: ""}); err == nil {
		t.Fatalf("缺主体应被拒")
	}
	if _, err := svc.ApprovalCenter(ListCenterView{View: "decided", Subject: "u"}); err != nil {
		t.Fatalf("合法调用不应报错: %v", err)
	}
}

// gormNotFound is a local stand-in so the fake doesn't import gorm.
func gormNotFound() error { return errPolicyNotFound{} }

type errPolicyNotFound struct{}

func (errPolicyNotFound) Error() string { return "record not found" }
