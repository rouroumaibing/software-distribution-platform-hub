// Package notification 实现通知中心（STATUS §2 #7）：顶栏铃铛消费的运行中心通知流。
//
// 当前唯一来源 = 待审批的流水线运行（pipeline_approvals.status = 'Pending'）。
// 这类通知是"行动项"——点开即跳到运行详情页去审批。后续若接入集群离线告警
// （#5）、制品 GC 对账（#8 无关）等，只需在 Service.List 里追加来源即可，
// 前端契约（Notification 形状）保持不变。
package notification

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"

	runmodels "github.com/rouroumaibing/software-distribution-platform-hub/internal/run/models"
)

// Notification 是通知中心的一条记录。前端铃铛按 Type 渲染图标，按 Link 跳转。
type Notification struct {
	ID        string    `json:"id"`
	Type      string    `json:"type"` // 当前仅 "approval"
	Title     string    `json:"title"`
	Body      string    `json:"body"`
	Link      string    `json:"link"` // 前端路由（SPA 内跳转）
	CreatedAt time.Time `json:"createdAt"`
}

// ApprovalSource 拉取待审批项。由 hub 的 PipelineApprovalRepository 满足。
type ApprovalSource interface {
	ListPending(limit int) ([]runmodels.PipelineApproval, error)
}

// RunLinkSource 解析运行归属的流水线，用于构造运行详情深链。
type RunLinkSource interface {
	PipelineIDOf(ctx context.Context, runID uuid.UUID) (uuid.UUID, error)
}

// Service 把若干通知来源聚合成统一流。
type Service struct {
	approvals ApprovalSource
	runs      RunLinkSource
}

func New(approvals ApprovalSource, runs RunLinkSource) *Service {
	return &Service{approvals: approvals, runs: runs}
}

// List 返回当前未读/待办通知。limit 越界时夹到 [1,100] 并默认 50。
func (s *Service) List(ctx context.Context, limit int) ([]Notification, error) {
	if limit <= 0 {
		limit = 50
	}
	if limit > 100 {
		limit = 100
	}
	pending, err := s.approvals.ListPending(limit)
	if err != nil {
		return nil, err
	}
	out := make([]Notification, 0, len(pending))
	for _, a := range pending {
		pid, perr := s.runs.PipelineIDOf(ctx, a.RunID)
		link := "/runs/" + a.RunID.String()
		if perr == nil && pid != uuid.Nil {
			link = "/pipelines/" + pid.String() + "/runs/" + a.RunID.String()
		}
		// RequestedBy 是 token sub，可能带 issuer 后缀；前端只展示，这里不做裁剪。
		body := "有一条等待审批的运行"
		if a.RequestedBy != "" {
			body = "发起人 " + a.RequestedBy + " 发起的运行等待您审批"
		}
		if c := strings.TrimSpace(a.ComponentID.String()); c != uuid.Nil.String() {
			body += "（组件 " + c[:8] + "）"
		}
		out = append(out, Notification{
			ID:        a.ID.String(),
			Type:      "approval",
			Title:     "待审批的流水线运行",
			Body:      body,
			Link:      link,
			CreatedAt: a.CreatedAt,
		})
	}
	return out, nil
}
