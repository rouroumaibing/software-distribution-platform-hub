package service

import (
	"encoding/json"
	"fmt"

	"github.com/google/uuid"

	"github.com/rouroumaibing/software-distribution-platform-hub/internal/run/models"
)

// 审批配置与审批中心（ui-12 §5.1 / 决策 #4 / P1 契约缺口：
// Assignee 字段、策略实体、assignee=me 视图）。
//
// 范围注记：策略实体支持 组件默认（environment_id NULL）+ 环境覆盖 两档。
// 执行路径（2026-10-11 接通）：pipeline_runs 反规范化 org_id/environment_id，
// 触发时按 (component, target, namespace) 唯一命中解析环境；命中且存在环境
// 覆盖档 → 用覆盖档，否则回退组件默认档（applyApprovalPolicy）。

// ApprovalPolicyStore persists approval policy rows.
type ApprovalPolicyStore interface {
	UpsertPolicy(*models.ApprovalPolicy) error
	ListPolicies(componentID uuid.UUID) ([]models.ApprovalPolicy, error)
	PolicyFor(componentID, environmentID uuid.UUID) (*models.ApprovalPolicy, error)
}

// SetApprovalPolicyStore wires the policy store; nil (default) disables
// policy-driven assignee seeding and the policy endpoints degrade to empty.
func (s *PipelineRunService) SetApprovalPolicyStore(store ApprovalPolicyStore) {
	s.approvalPolicyStore = store
}

// ListCenterView is one of the approval center's three views.
type ListCenterView struct {
	View    string // pending | initiated | decided
	Subject string
	Limit   int
}

// ApprovalCenter returns the rows of one approval-center view for a subject.
func (s *PipelineRunService) ApprovalCenter(v ListCenterView) ([]models.PipelineApproval, error) {
	switch v.View {
	case "pending", "initiated", "decided":
	default:
		return nil, fmt.Errorf("view 须为 pending / initiated / decided")
	}
	if v.Subject == "" {
		return nil, fmt.Errorf("审批中心须携带主体（登录态）")
	}
	if v.Limit <= 0 || v.Limit > 200 {
		v.Limit = 50
	}
	return s.approvalRepo.ListCenter(v.View, v.Subject, v.Limit)
}

// GetApprovalPolicies lists a component's policy rows (default + overrides).
func (s *PipelineRunService) GetApprovalPolicies(componentID uuid.UUID) ([]models.ApprovalPolicy, error) {
	if s.approvalPolicyStore == nil {
		return []models.ApprovalPolicy{}, nil
	}
	return s.approvalPolicyStore.ListPolicies(componentID)
}

// UpsertApprovalPolicy validates and writes one policy row.
func (s *PipelineRunService) UpsertApprovalPolicy(p *models.ApprovalPolicy, subject string) error {
	if s.approvalPolicyStore == nil {
		return fmt.Errorf("审批策略存储未装配")
	}
	if !models.ValidApprovalMode(p.Mode) {
		return fmt.Errorf("mode 须为 any / all / n")
	}
	if p.Mode == "n" && p.MinApprovals < 1 {
		return fmt.Errorf("mode=n 时 minApprovals 须 ≥ 1")
	}
	if p.Assignees != "" {
		var names []string
		if err := json.Unmarshal([]byte(p.Assignees), &names); err != nil {
			return fmt.Errorf("assignees 须为字符串数组 JSON")
		}
		if len(names) == 0 {
			p.Assignees = ""
		}
	}
	p.CreatedBy = subject
	return s.approvalPolicyStore.UpsertPolicy(p)
}

// applyApprovalPolicy stamps the policy's assignee list onto a freshly seeded
// approval row. 两档解析（ui-12 §5.1）：run 已绑定环境且存在**环境覆盖档**时用
// 覆盖档；否则回退组件默认档。Best-effort: policy store absence/misconfig
// falls back to "any approver" (空指派) without failing the trigger.
func (s *PipelineRunService) applyApprovalPolicy(componentID uuid.UUID, environmentID *uuid.UUID, pa *models.PipelineApproval) {
	if s.approvalPolicyStore == nil {
		return
	}
	if environmentID != nil {
		if policy, err := s.approvalPolicyStore.PolicyFor(componentID, *environmentID); err == nil && policy != nil {
			pa.Assignee = policy.Assignees
			return
		}
	}
	policy, err := s.approvalPolicyStore.PolicyFor(componentID, uuid.Nil)
	if err != nil || policy == nil {
		return // 无策略 = 任意审批人
	}
	pa.Assignee = policy.Assignees
}
