package service

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"
	"gorm.io/gorm"

	"github.com/rouroumaibing/software-distribution-platform-hub/internal/common"
	applog "github.com/rouroumaibing/software-distribution-platform-hub/internal/common/logger"
	"github.com/rouroumaibing/software-distribution-platform-hub/internal/pipeline/cronexpr"
	"github.com/rouroumaibing/software-distribution-platform-hub/internal/pipeline/models"
	runmodels "github.com/rouroumaibing/software-distribution-platform-hub/internal/run/models"
)

// RunStarter is the slice of PipelineRunService the trigger service needs.
// An interface (not the concrete type) keeps pipeline → run a one-way,
// wiring-time dependency — run already depends on pipeline, so the reverse
// hard reference would be an import cycle (same pattern as run's VarSource).
type RunStarter interface {
	Trigger(pipelineID uuid.UUID, req *runmodels.TriggerRequest) ([]*runmodels.PipelineRun, error)
}

// TriggerStore is the persistence surface of the trigger service. The gorm
// repository implements it; tests substitute fakes.
type TriggerStore interface {
	Create(*models.PipelineTrigger) error
	GetByID(uuid.UUID) (*models.PipelineTrigger, error)
	Update(*models.PipelineTrigger) error
	Delete(uuid.UUID) error
	FindByPipeline(uuid.UUID, common.Pagination) ([]models.PipelineTrigger, int64, error)
	FindEnabledByType(string) ([]models.PipelineTrigger, error)
	ClaimDueCron(tx *gorm.DB) ([]models.PipelineTrigger, error)
	UpdateLastTriggered(tx *gorm.DB, id uuid.UUID, at time.Time) error
}

// TriggerService manages PipelineTrigger rows (ui-12 §4.3 / N-21): CRUD +
// validation, the in-built cron scheduler (ticker + FOR UPDATE SKIP LOCKED
// claim), webhook token lifecycle, and run.finished event evaluation.
type TriggerService struct {
	repo    TriggerStore
	db      *gorm.DB
	starter RunStarter
	now     func() time.Time
}

func NewTriggerService(repo TriggerStore, db *gorm.DB, starter RunStarter) *TriggerService {
	return &TriggerService{repo: repo, db: db, starter: starter, now: time.Now}
}

// ---- CRUD -----------------------------------------------------------------

func validTriggerType(t string) bool {
	return t == models.TriggerTypeCron || t == models.TriggerTypeWebhook || t == models.TriggerTypeEvent
}

// cronExprOf extracts and validates the cron expression from Spec.
func cronExprOf(spec datatypes.JSON) (*cronexpr.Expr, error) {
	var body struct {
		Expr string `json:"expr"`
	}
	if len(spec) > 0 {
		if err := json.Unmarshal(spec, &body); err != nil {
			return nil, fmt.Errorf("spec 不是合法 JSON: %w", err)
		}
	}
	if body.Expr == "" {
		return nil, fmt.Errorf("cron 触发器须声明 spec.expr")
	}
	return cronexpr.Parse(body.Expr)
}

// validate checks type-specific Spec; returns an error the handler maps to
// 400 (字段级即时校验的契约：非法 cron 在保存时就拒绝，不等到第一次触发).
func validate(t string, spec datatypes.JSON) error {
	switch t {
	case models.TriggerTypeCron:
		_, err := cronExprOf(spec)
		return err
	case models.TriggerTypeWebhook:
		return nil
	case models.TriggerTypeEvent:
		var es models.EventSpec
		if len(spec) == 0 {
			return fmt.Errorf("event 触发器须声明 spec.onPipelineId")
		}
		if err := json.Unmarshal(spec, &es); err != nil {
			return fmt.Errorf("spec 不是合法 JSON: %w", err)
		}
		if es.OnPipelineID == uuid.Nil {
			return fmt.Errorf("event 触发器须声明 spec.onPipelineId")
		}
		return nil
	}
	return fmt.Errorf("未知触发器类型 %q", t)
}

func (s *TriggerService) Create(pipelineID uuid.UUID, trig *models.PipelineTrigger, createdBy string) (*models.PipelineTrigger, error) {
	if trig.Name == "" {
		return nil, fmt.Errorf("触发器名称不能为空")
	}
	if !validTriggerType(trig.Type) {
		return nil, fmt.Errorf("触发器类型须为 cron / webhook / event")
	}
	if err := validate(trig.Type, trig.Spec); err != nil {
		return nil, err
	}
	trig.PipelineID = pipelineID
	trig.CreatedBy = createdBy
	if trig.Type == models.TriggerTypeWebhook {
		token, err := newWebhookToken()
		if err != nil {
			return nil, err
		}
		trig.Token = token
	}
	if err := s.repo.Create(trig); err != nil {
		return nil, err
	}
	return trig, nil
}

func (s *TriggerService) Get(id uuid.UUID) (*models.PipelineTrigger, error) {
	return s.repo.GetByID(id)
}

func (s *TriggerService) List(pipelineID uuid.UUID, p common.Pagination) ([]models.PipelineTrigger, int64, error) {
	return s.repo.FindByPipeline(pipelineID, p)
}

// Update patches editable fields (name/enabled/spec); type is immutable.
func (s *TriggerService) Update(id uuid.UUID, patch *models.PipelineTrigger) (*models.PipelineTrigger, error) {
	existing, err := s.repo.GetByID(id)
	if err != nil {
		return nil, err
	}
	if patch.Name != "" {
		existing.Name = patch.Name
	}
	if patch.Spec != nil && len(patch.Spec) > 0 {
		if err := validate(existing.Type, patch.Spec); err != nil {
			return nil, err
		}
		existing.Spec = patch.Spec
	}
	existing.Enabled = patch.Enabled
	if err := s.repo.Update(existing); err != nil {
		return nil, err
	}
	return existing, nil
}

func (s *TriggerService) Delete(id uuid.UUID) error { return s.repo.Delete(id) }

// Rotate regenerates a webhook trigger's token; the new value is returned
// exactly once (展示一次 + 轮换 的契约).
func (s *TriggerService) Rotate(id uuid.UUID) (string, error) {
	existing, err := s.repo.GetByID(id)
	if err != nil {
		return "", err
	}
	if existing.Type != models.TriggerTypeWebhook {
		return "", fmt.Errorf("仅 webhook 触发器可轮换签名")
	}
	token, err := newWebhookToken()
	if err != nil {
		return "", err
	}
	existing.Token = token
	if err := s.repo.Update(existing); err != nil {
		return "", err
	}
	return token, nil
}

// AuthorizeWebhook constant-time checks a presented token against a trigger.
func (s *TriggerService) AuthorizeWebhook(id uuid.UUID, presented string) (*models.PipelineTrigger, bool) {
	trig, err := s.repo.GetByID(id)
	if err != nil || trig.Type != models.TriggerTypeWebhook || trig.Token == "" || presented == "" {
		return nil, false
	}
	if subtle.ConstantTimeCompare([]byte(presented), []byte(trig.Token)) != 1 {
		return nil, false
	}
	return trig, true
}

// PreviewCron parses expr and returns the next n fire times (console 的
// 未来 5 次预览 / 非法即时校验端点).
func (s *TriggerService) PreviewCron(expr string, n int) ([]time.Time, error) {
	parsed, err := cronexpr.Parse(expr)
	if err != nil {
		return nil, err
	}
	if n <= 0 || n > 10 {
		n = 5
	}
	return parsed.NextN(s.now(), n)
}

// newWebhookToken mints 128-bit of entropy, hex-encoded with a wh_ prefix.
func newWebhookToken() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("生成 webhook token 失败: %w", err)
	}
	return "wh_" + hex.EncodeToString(b), nil
}

// ---- cron scheduler ---------------------------------------------------------

// StartScheduler runs the cron tick loop until ctx is cancelled. Interval is
// the tick cadence (default 1 minute; tests pass shorter values).
func (s *TriggerService) StartScheduler(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = time.Minute
	}
	ticker := time.NewTicker(interval)
	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				s.tick()
			}
		}
	}()
}

// tick claims enabled cron triggers under FOR UPDATE SKIP LOCKED, fires the
// due ones and stamps LastTriggeredAt in the same tx. A failed fire does NOT
// stamp, so the next tick retries (missed-fire = retry, not skip).
func (s *TriggerService) tick() {
	trigs, err := s.repo.ClaimDueCron(s.db)
	if err != nil {
		applog.Warnf("trigger: cron claim failed: %v", err)
		return
	}
	now := s.now()
	for i := range trigs {
		trig := &trigs[i]
		expr, err := cronExprOf(trig.Spec)
		if err != nil {
			// 行内脏数据：记日志、跳过，不让一条坏数据毒死整个 tick。
			applog.Warnf("trigger %s (%s): bad cron spec, skipped: %v", trig.ID, trig.Name, err)
			continue
		}
		base := now.Add(-time.Minute)
		if trig.LastTriggeredAt != nil {
			base = *trig.LastTriggeredAt
		} else {
			base = trig.CreatedAt
		}
		next, err := expr.Next(base)
		if err != nil || next.After(now) {
			continue // not due
		}
		if err := s.fire(trig, "cron"); err != nil {
			applog.Warnf("trigger %s (%s): fire failed (will retry next tick): %v", trig.ID, trig.Name, err)
			continue
		}
		if err := s.repo.UpdateLastTriggered(s.db, trig.ID, now); err != nil {
			applog.Warnf("trigger %s (%s): stamp last_triggered_at failed: %v", trig.ID, trig.Name, err)
		}
	}
}

// fire starts a run via the wired RunStarter. Source名 becomes TriggeredBy
// ("cron:<名称>" / "webhook:<名称>" / "event:<名称>").
func (s *TriggerService) fire(trig *models.PipelineTrigger, source string) error {
	if s.starter == nil {
		return fmt.Errorf("trigger: RunStarter 未装配")
	}
	req := &runmodels.TriggerRequest{
		TriggeredBy:   source + ":" + trig.Name,
		TriggerSource: source,
	}
	runs, err := s.starter.Trigger(trig.PipelineID, req)
	if err != nil {
		return err
	}
	applog.Infof("trigger: %s trigger %q fired pipeline %s → %d run(s)", source, trig.Name, trig.PipelineID, len(runs))
	return nil
}

// InvokeWebhook fires an already-token-authorized webhook trigger and stamps
// its LastTriggeredAt. Split from AuthorizeWebhook so the handler keeps the
// auth/enable checks and the service owns firing.
func (s *TriggerService) InvokeWebhook(trig *models.PipelineTrigger) error {
	if err := s.fire(trig, "webhook"); err != nil {
		return err
	}
	return s.repo.UpdateLastTriggered(s.db, trig.ID, s.now())
}

// ---- run.finished event -----------------------------------------------------

// EvaluateRunFinished fires every enabled event trigger watching pipelineID
// whose status filter matches the just-finished phase. Best-effort: failures
// log and do not propagate (状态回写路径不容触发器故障毒化).
func (s *TriggerService) EvaluateRunFinished(finishedPipelineID uuid.UUID, phase string) {
	trigs, err := s.repo.FindEnabledByType(models.TriggerTypeEvent)
	if err != nil {
		applog.Warnf("trigger: event scan failed: %v", err)
		return
	}
	for i := range trigs {
		trig := &trigs[i]
		spec := models.DecodeEventSpec(trig.Spec)
		if spec.OnPipelineID != finishedPipelineID {
			continue
		}
		matched := false
		for _, st := range spec.Statuses {
			if st == phase {
				matched = true
				break
			}
		}
		if !matched {
			continue
		}
		if err := s.fire(trig, "event"); err != nil {
			applog.Warnf("trigger %s (%s): event fire failed: %v", trig.ID, trig.Name, err)
			continue
		}
		if err := s.repo.UpdateLastTriggered(s.db, trig.ID, s.now()); err != nil {
			applog.Warnf("trigger %s (%s): stamp last_triggered_at failed: %v", trig.ID, trig.Name, err)
		}
	}
}
