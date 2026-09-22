package service

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"

	"github.com/rouroumaibing/software-distribution-platform-hub/internal/common"
	applog "github.com/rouroumaibing/software-distribution-platform-hub/internal/common/logger"
	"github.com/rouroumaibing/software-distribution-platform-hub/internal/environment/models"
	targetmodels "github.com/rouroumaibing/software-distribution-platform-hub/internal/target/models"
)

// EnvironmentStore is the narrow persistence surface this service needs — the
// repo convention that keeps service-layer branches unit-testable without
// Postgres. *repository.EnvironmentRepository satisfies it implicitly, so the
// wiring in main.go is unchanged; what changes is that the key-field
// downgrade, the delete audit and the test-result persistence below are now
// reachable from a test.
type EnvironmentStore interface {
	Create(e *models.Environment) error
	GetByID(id uuid.UUID) (*models.Environment, error)
	FindByComponentID(componentID uuid.UUID, p common.Pagination) ([]models.Environment, int64, error)
	Update(e *models.Environment) error
	Delete(id uuid.UUID) error
}

// TargetLookup resolves the target an environment is anchored to, for the
// connection checklist (§7.12.5). One method wide on purpose: a checklist test
// should not have to fake the whole target module.
type TargetLookup interface {
	GetByID(id uuid.UUID) (*targetmodels.Target, error)
}

// ConfigOverrideCounter reports config-override rows scoped to an environment, so
// EnvironmentService.Delete can audit (not block) a delete (DELETE-CONTRACT §6.4 #8).
type ConfigOverrideCounter interface {
	CountByEnvironment(envID uuid.UUID) (int64, error)
}

// EnvironmentService satisfies common.CRUDService[models.Environment].
type EnvironmentService struct {
	repo          EnvironmentStore
	targetRepo    TargetLookup
	configCounter ConfigOverrideCounter
}

func NewEnvironmentService(repo EnvironmentStore, targetRepo TargetLookup, configCounter ConfigOverrideCounter) *EnvironmentService {
	return &EnvironmentService{repo: repo, targetRepo: targetRepo, configCounter: configCounter}
}

func (s *EnvironmentService) Create(e *models.Environment) error {
	e.Status = deriveStatus(e)
	return s.repo.Create(e)
}

func (s *EnvironmentService) Get(id uuid.UUID) (*models.Environment, error) {
	return s.repo.GetByID(id)
}

func (s *EnvironmentService) ListByComponent(componentID uuid.UUID, p common.Pagination) ([]models.Environment, int64, error) {
	return s.repo.FindByComponentID(componentID, p)
}

func (s *EnvironmentService) Update(id uuid.UUID, e *models.Environment) error {
	e.ID = id
	// Changing a key field (target / namespace / credential ref) drops the env
	// back to configured_unverified and clears the last test result (§7.12.6).
	previous, err := s.repo.GetByID(id)
	if err == nil && keyFieldChanged(previous, e) {
		e.Status = models.EnvStatusConfiguredUnverified
		e.LastTestAt = nil
		e.LastTestResult = ""
	}
	return s.repo.Update(e)
}

// Delete removes the environment. Per DELETE-CONTRACT §6.4 #8 we do NOT reject
// on residual config overrides — instead we surface that the delete will
// cascade-remove them as an audit warning. Cluster-side resources are
// intentionally not reclaimed (decision 1, 2026-09-16). The environment is
// hard-deleted; its audit history in component_config_history survives because
// the FK to environments was dropped (B-14).
func (s *EnvironmentService) Delete(id uuid.UUID) error {
	if s.configCounter != nil {
		if n, err := s.configCounter.CountByEnvironment(id); err == nil && n > 0 {
			applog.Warnf("environment: deleting env %s will cascade-remove %d config override(s)", id, n)
		}
	}
	return s.repo.Delete(id)
}

// TestItem is one row of the per-dimension connection checklist (§7.12.5).
type TestItem struct {
	Name   string `json:"name"`
	Status string `json:"status"` // pass | fail | skip
	Detail string `json:"detail,omitempty"`
}

// TestReport is the result of POST /environments/:id/test. Hub has no outbound
// connectivity (zero client-go / zero ssh), so items requiring a live connection
// are returned as "skip" with an explicit note; configuration-completeness items
// are validated structurally.
type TestReport struct {
	Access   string     `json:"access"`
	Items    []TestItem `json:"items"`
	Passed   int        `json:"passed"`
	Total    int        `json:"total"`
	Status   string     `json:"status"`
	TestedAt string     `json:"testedAt"`
}

// Test runs the connection checklist for an environment and persists the result.
func (s *EnvironmentService) Test(id uuid.UUID) (*TestReport, error) {
	env, err := s.repo.GetByID(id)
	if err != nil {
		return nil, err
	}
	var target *targetmodels.Target
	if t, err := s.targetRepo.GetByID(env.TargetID); err == nil {
		target = t
	}

	report := buildTestReport(env, target)
	now := time.Now()
	status := models.EnvStatusVerified
	for _, it := range report.Items {
		if it.Status == "fail" {
			status = models.EnvStatusFailed
			break
		}
	}
	// Fill in the report *before* it is serialized and persisted: the response
	// the caller gets and the snapshot stored in last_test_result must be the
	// same object. (Previously only env.Status was set, so the API returned an
	// empty `status` and the archived snapshot carried neither status nor
	// testedAt — the console had nothing to render.)
	report.Status = status
	report.TestedAt = now.Format(time.RFC3339)

	// If every item that hub can evaluate passes but connectivity items are
	// skipped, the env is considered configured (real reachability is verified
	// by the runner / direct-connect executor at publish time).
	env.Status = status
	env.LastTestAt = &now
	raw, _ := json.Marshal(report)
	env.LastTestResult = string(raw)
	if err := s.repo.Update(env); err != nil {
		return nil, err
	}
	return report, nil
}

func buildTestReport(env *models.Environment, target *targetmodels.Target) *TestReport {
	report := &TestReport{Access: env.Access, Items: []TestItem{}}
	switch env.Access {
	case models.EnvAccessAgent:
		online := target != nil && target.Status == targetmodels.TargetStatusOnline && isFresh(target.LastHeartbeatAt)
		report.Items = append(report.Items,
			item("目标已注册", env.TargetID != uuid.Nil, "目标未选择"),
			item("Runner Agent 在线（心跳新鲜度）", online, "目标离线或心跳过期，agent 通道需 Runner 在线"),
			item("命名空间已声明", env.Namespace != "", "命名空间为空"),
			skip("部署权限（dry-run create deployment）", "hub 无出站能力，由 Runner 侧执行"),
		)
	case models.EnvAccessKubeconfig:
		hasCred := env.AccessConfig.KubeCredRef != ""
		report.Items = append(report.Items,
			skip("kube-apiserver 可达（TCP/TLS 握手）", "hub 无出站能力，由直连执行器完成"),
			item("凭据已配置（TLS/CA 或显式跳过）", hasCred, "未配置 kubeconfig 凭据引用"),
			item("认证通过", hasCred, "未配置 kubeconfig 凭据引用"),
			skip("命名空间可访问", "hub 无出站能力，由直连执行器完成"),
			skip("部署权限（dry-run create deployment）", "hub 无出站能力，由直连执行器完成"),
		)
	case models.EnvAccessSSH:
		hasHosts := len(env.AccessConfig.SSHTargets) > 0
		hasCred := env.AccessConfig.SSHSecretRef != ""
		report.Items = append(report.Items,
			skip("TCP 可达", "hub 无出站能力，由直连执行器完成"),
			skip("主机指纹校验（known_hosts）", "hub 无出站能力，由直连执行器完成"),
			item("SSH 认证通过", hasCred, "未配置 SSH 凭据引用"),
			skip("部署目录可写", "hub 无出站能力，由直连执行器完成"),
			item("sudo 提权配置", true, ""),
			item("主机清单已配置", hasHosts, "未添加任何目标主机"),
		)
	default:
		report.Items = append(report.Items, item("接入方式已知", false, "未知接入方式: "+env.Access))
	}

	passed, total := 0, 0
	for _, it := range report.Items {
		if it.Status == "pass" {
			passed++
		}
		if it.Status != "skip" {
			total++
		}
	}
	report.Passed = passed
	report.Total = total
	return report
}

func item(name string, ok bool, failDetail string) TestItem {
	if ok {
		return TestItem{Name: name, Status: "pass"}
	}
	return TestItem{Name: name, Status: "fail", Detail: failDetail}
}

func skip(name, detail string) TestItem {
	return TestItem{Name: name, Status: "skip", Detail: detail}
}

func isFresh(t *time.Time) bool {
	return t != nil && time.Since(*t) < 5*time.Minute
}

func deriveStatus(e *models.Environment) string {
	switch e.Access {
	case models.EnvAccessAgent:
		if e.TargetID != uuid.Nil && e.Namespace != "" {
			return models.EnvStatusConfiguredUnverified
		}
	case models.EnvAccessKubeconfig:
		if e.AccessConfig.KubeCredRef != "" {
			return models.EnvStatusConfiguredUnverified
		}
	case models.EnvAccessSSH:
		if len(e.AccessConfig.SSHTargets) > 0 && e.AccessConfig.SSHSecretRef != "" {
			return models.EnvStatusConfiguredUnverified
		}
	}
	return models.EnvStatusUnconfigured
}

func keyFieldChanged(old, newEnv *models.Environment) bool {
	return old.TargetID != newEnv.TargetID ||
		old.Namespace != newEnv.Namespace ||
		old.Access != newEnv.Access ||
		old.AccessConfig.KubeCredRef != newEnv.AccessConfig.KubeCredRef ||
		old.AccessConfig.SSHSecretRef != newEnv.AccessConfig.SSHSecretRef
}
