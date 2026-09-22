package service

import (
	"fmt"
	"net/http"

	"github.com/google/uuid"

	runnerapi "github.com/rouroumaibing/software-distribution-platform-runner/api/v1alpha1"

	"github.com/rouroumaibing/software-distribution-platform-hub/internal/common"
)

// ProductionPolicy is the environment-facing slice the mandatory-approval guard
// needs: "which of these targets are production environments?".
//
// 抽成窄接口（而不是直接依赖 *environment/repository）与仓内既有手法一致，
// 也让"拒 / 放"两条分支能脱离 Postgres 单测。
type ProductionPolicy interface {
	// ProductionTargets returns the subset of targetIDs that at least one
	// environment marks EnvType=production.
	ProductionTargets(targetIDs []uuid.UUID) ([]uuid.UUID, error)
}

// SetProductionPolicy wires the 生产强审批 guard (B-11). Optional: nil means
// "no platform-level production policy" — 与鉴权默认关闭时管理面裸挂同一口径，
// 不在未装配时静默放行任何本该拒绝的请求（因为根本没有策略可查）。
func (s *PipelineRunService) SetProductionPolicy(p ProductionPolicy) { s.productionPolicy = p }

// enforceProductionApproval implements 生产强审批 (B-11 / Epic 8).
//
// 规则（**平台级**，独立于流水线作者怎么配）：向生产环境触发的运行，其定义必须
// 至少包含一个人工审核任务。理由是这类操作不可逆的代价最高，且不该由"作者忘了加
// 审批"来决定有没有卡点 —— environment 模型的 EnvType 注释里写的就是这个用途。
//
// 判定分两层：
//   - 纯逻辑 `HasApprovalGate`（无 DB、可单测）决定"这份定义有没有卡点"；
//   - 窄接口查"这些 target 里哪些是生产"。
//
// **失败即拒绝**（fail-closed）：策略查询报错时不放行。这里选保守方向，是因为
// 放行一个本该被审批卡住的**生产**发布，代价远大于一次"稍后重试触发"。
func (s *PipelineRunService) enforceProductionApproval(spec *runnerapi.PipelineRunSpec, targetIDs []uuid.UUID) error {
	if s.productionPolicy == nil || len(targetIDs) == 0 {
		return nil
	}
	if HasApprovalGate(spec.Tasks) {
		return nil
	}
	prodTargets, err := s.productionPolicy.ProductionTargets(targetIDs)
	if err != nil {
		return fmt.Errorf("cannot evaluate the production approval policy (refusing to trigger): %w", err)
	}
	if len(prodTargets) == 0 {
		return nil
	}
	names := make([]string, 0, len(prodTargets))
	for _, id := range prodTargets {
		names = append(names, id.String())
	}
	return common.DomainErrorWithReasons(common.KindPipeline, http.StatusConflict, 5,
		"production deployments require an approval gate",
		fmt.Sprintf("目标环境 %s 属于生产环境（envType=production），流水线必须包含至少一个人工审核阶段", fmt.Sprint(names)))
}

// productionGuardTargets 收敛两种触发形态的 target 集合，让守卫只有一处调用点。
func productionGuardTargets(single uuid.UUID, many []uuid.UUID) []uuid.UUID {
	if len(many) > 0 {
		return many
	}
	if single == uuid.Nil {
		return nil
	}
	return []uuid.UUID{single}
}
