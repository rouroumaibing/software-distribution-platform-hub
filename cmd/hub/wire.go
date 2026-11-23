package main

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/rouroumaibing/software-distribution-platform-hub/internal/common"
	applog "github.com/rouroumaibing/software-distribution-platform-hub/internal/common/logger"
	config "github.com/rouroumaibing/software-distribution-platform-hub/internal/config"
	pipelinesvc "github.com/rouroumaibing/software-distribution-platform-hub/internal/pipeline/service"
	"github.com/rouroumaibing/software-distribution-platform-hub/internal/platform"
	retentionsvc "github.com/rouroumaibing/software-distribution-platform-hub/internal/retention"
	runsvc "github.com/rouroumaibing/software-distribution-platform-hub/internal/run/service"
	targetsvc "github.com/rouroumaibing/software-distribution-platform-hub/internal/target/service"

	artifactsvc "github.com/rouroumaibing/software-distribution-platform-hub/internal/artifact/service"
	artifactstorage "github.com/rouroumaibing/software-distribution-platform-hub/internal/artifact/storage"
)

// artifactWiring is the three views of the configured object store: the
// general client, the concrete local driver (for signed routes), and the
// delete-only capability used by retention GC.
type artifactWiring struct {
	client  artifactstorage.Client
	local   *artifactstorage.Local
	deleter artifactsvc.ObjectDeleter
}

// objectDeleterFromDrivers converts concrete driver pointers, never a possibly
// nil interface. A typed-nil pointer stays nil here, so GC cannot mistake an
// unconfigured store for a live one.
func objectDeleterFromDrivers(local *artifactstorage.Local, s3 *artifactstorage.S3) artifactsvc.ObjectDeleter {
	if local != nil {
		return local
	}
	if s3 != nil {
		return s3
	}
	return nil
}

// wireArtifactStorage assembles the configured object store plus the concrete
// driver pointers used by routes and by retention GC.
func wireArtifactStorage(cfg *config.Config) artifactWiring {
	var w artifactWiring
	var s3Store *artifactstorage.S3
	switch cfg.ArtifactStoreDriver {
	case "s3":
		store, err := artifactstorage.NewS3(artifactstorage.S3Config{
			Endpoint:     cfg.ArtifactStoreEndpoint,
			AccessKey:    cfg.ArtifactStoreAccessKey,
			SecretKey:    cfg.ArtifactStoreSecretKey,
			SessionToken: cfg.ArtifactStoreSessionTok,
			Bucket:       cfg.ArtifactStoreBucket,
			Region:       cfg.ArtifactStoreRegion,
			Secure:       cfg.ArtifactStoreSecure,
		})
		if err != nil {
			applog.Fatalf("hub: invalid artifact S3 config: %v", err)
		}
		s3Store = store
		w.client = store
		applog.Infof("hub: artifact storage driver=s3 bucket=%s endpoint=%s", cfg.ArtifactStoreBucket, cfg.ArtifactStoreEndpoint)
	case "local":
		if cfg.ArtifactStoreSignSecret == "" {
			applog.Fatalf("hub: ARTIFACT_STORE_DRIVER=local requires ARTIFACT_STORE_SIGN_SECRET; GATEWAY_TOKEN is not a substitute")
		}
		store, err := artifactstorage.NewLocal(cfg.ArtifactStoreLocalRoot, cfg.ArtifactStoreSignSecret, cfg.ArtifactStorePublicURL)
		if err != nil {
			applog.Fatalf("hub: cannot init local artifact storage at %s: %v", cfg.ArtifactStoreLocalRoot, err)
		}
		w.local = store
		w.client = store
		applog.Infof("hub: artifact storage driver=local root=%s public=%s", cfg.ArtifactStoreLocalRoot, cfg.ArtifactStorePublicURL)
	default:
		if cfg.ArtifactStoreDriver != "" {
			applog.Fatalf("hub: unknown ARTIFACT_STORE_DRIVER %q (want s3|local)", cfg.ArtifactStoreDriver)
		}
		applog.Info("hub: artifact storage not configured — downloads/uploads will 503 until ARTIFACT_STORE_DRIVER is set")
	}
	w.deleter = objectDeleterFromDrivers(w.local, s3Store)
	return w
}

// artifactBackgroundRepo is the metadata surface both background jobs need.
type artifactBackgroundRepo interface {
	artifactsvc.StorageKeySource
	artifactsvc.ExpiredArtifactSource
}

// wireArtifactBackground starts the report-only orphan reconciliation and the
// delete-only retention GC. The deleter is explicit and nil when no driver was built.
func wireArtifactBackground(ctx context.Context, cfg *config.Config, repo artifactBackgroundRepo, w artifactWiring) {
	if enumerator, ok := w.client.(artifactstorage.Enumerator); ok {
		reconciler := artifactsvc.NewArtifactReconciler(repo, enumerator, cfg.ArtifactReconcilePrefix)
		go reconciler.Run(ctx, cfg.ArtifactReconcileInterval)
		if cfg.ArtifactReconcileInterval > 0 {
			applog.Infof("hub: artifact orphan reconciliation enabled every %s (prefix=%q, report-only)",
				cfg.ArtifactReconcileInterval, cfg.ArtifactReconcilePrefix)
		}
	} else if w.client != nil {
		applog.Warnf("hub: artifact storage driver does not support listing — orphan reconciliation (B-16) is skipped, not reported as clean")
	}

	gc := artifactsvc.NewArtifactGC(repo, w.deleter, cfg.ArtifactGCBatch)
	go gc.Run(ctx, cfg.ArtifactGCInterval)
	if cfg.ArtifactGCInterval > 0 {
		batch := cfg.ArtifactGCBatch
		if batch <= 0 {
			batch = artifactsvc.DefaultArtifactGCBatch
		}
		applog.Infof("hub: artifact retention GC enabled every %s (batch=%d, objectStore=%v)",
			cfg.ArtifactGCInterval, batch, w.deleter != nil)
	}
}

// wireRetentionGC assembles the data-lifecycle GC from explicit config values.
func wireRetentionGC(ctx context.Context, cfg *config.Config, gdb *gorm.DB) *retentionsvc.RetentionGC {
	gc := retentionsvc.NewRetentionGC(gdb,
		cfg.AuditLogRetentionDays, cfg.RunLogRetentionDays,
		cfg.ArtifactVersionRetention, cfg.RetentionGCBatch)
	go gc.Run(ctx, cfg.RetentionGCInterval)
	if !gc.Disabled() {
		applog.Infof("hub: data-lifecycle retention GC enabled (auditDays=%d runDays=%d artifactVersions=%d)",
			gc.AuditLogDays, gc.RunLogDays, gc.ArtifactVersions)
	}
	return gc
}

// wireAgentOpReconciliation runs the two best-effort timeout sweeps for the
// agent operation ledger.
func wireAgentOpReconciliation(ctx context.Context, agentOpSvc *targetsvc.AgentOpService) {
	go func() {
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := agentOpSvc.SweepUpgradeTimeouts(ctx, 15*time.Minute); err != nil {
					applog.Infof("agentop: upgrade timeout sweep: %v", err)
				}
				if err := agentOpSvc.SweepApprovalTimeouts(ctx, 24*time.Hour); err != nil {
					applog.Infof("agentop: approval timeout sweep: %v", err)
				}
			}
		}
	}()
}

// changeWindowGuard reads platform settings on every trigger so a window
// change takes effect without a restart. A read failure is fail-open: this
// guard controls timing, so blocking all triggers on a DB fault widens the
// blast radius.
func changeWindowGuard(store platform.SettingStore) func() error {
	return func() error {
		cw, err := platform.LoadChangeWindow(store)
		if err != nil {
			applog.Warnf("platform: change window read failed, allowing trigger: %v", err)
			return nil
		}
		if platform.InWindow(cw, time.Now()) {
			return nil
		}
		return common.DomainErrorWithReasons(common.KindPipeline, http.StatusConflict, 6,
			"outside the change window",
			fmt.Sprintf("当前不在变更窗口内（%s–%s，星期 %v），触发已被平台变更窗口拦截",
				cw.Start, cw.End, cw.Weekdays))
	}
}

// runServiceDeps groups the required stores and the explicitly wired optional
// collaborators for the run service. Interfaces mirror the run service's own
// narrow ports, so the wiring layer owns no business logic.
type runServiceDeps struct {
	gdb              *gorm.DB
	runRepo          runsvc.PipelineRunStore
	taskRepo         runsvc.TaskRunStore
	pipelineRepo     runsvc.PipelineDefStore
	stageRepo        runsvc.StageStore
	taskTemplateRepo runsvc.TaskTemplateStore
	targetSvc        runsvc.TargetStore
	dispatcher       runsvc.Dispatcher
	dispatchRepo     runsvc.DispatchJobStore
	logRepo          runsvc.LogStore
	approvalRepo     runsvc.PipelineApprovalStore
	approvalPolicies runsvc.ApprovalPolicyStore
	componentMeta    runsvc.ComponentMetaStore
	rolloutStore     runsvc.RolloutSnapshotStore
	envMeta          runsvc.EnvironmentMetaStore
	production       runsvc.ProductionPolicy
	artifacts        runsvc.ArtifactRegistry
	vars             runsvc.VarSource
	componentGuard   runsvc.ComponentGuard
	notifySink       func(evt, subject, title, body string)
	settingStore     platform.SettingStore
	triggerRepo      pipelinesvc.TriggerStore
}

// wirePipelineRuntime builds the run service from one explicit Options bundle
// and starts its background loops. finishedHook is late-bound after the
// trigger service exists, breaking the trigger ↔ run construction cycle
// without a setter on the run service itself.
func wirePipelineRuntime(ctx context.Context, cfg *config.Config, deps runServiceDeps) (*runsvc.PipelineRunService, *pipelinesvc.TriggerService) {
	var finishedHook func(uuid.UUID, string)
	opts := runsvc.PipelineRunOptions{
		RolloutRefluxEnabled:          true,
		RolloutStore:                  deps.rolloutStore,
		EnvironmentAttributionEnabled: true,
		EnvironmentMetaStore:          deps.envMeta,
		ProductionApprovalEnabled:     true,
		ProductionPolicy:              deps.production,
		ArtifactRegistrationEnabled:   true,
		ArtifactRegistry:              deps.artifacts,
		VariableTierResolutionEnabled: true,
		VarSource:                     deps.vars,
		ApprovalPolicyEnabled:         true,
		ApprovalPolicyStore:           deps.approvalPolicies,
		ComponentGovernanceEnabled:    true,
		ComponentGuard:                deps.componentGuard,
		ChangeWindowGuardEnabled:      true,
		ChangeWindowGuard:             changeWindowGuard(deps.settingStore),
		RunFinishedHookEnabled:        true,
		RunFinishedHook: func(id uuid.UUID, phase string) {
			if finishedHook != nil {
				finishedHook(id, phase)
			}
		},
		NotifyEnabled:       true,
		NotifySink:          deps.notifySink,
		JobServiceAccount:   cfg.JobServiceAccount,
		RunConcurrencyLimit: cfg.RunConcurrencyLimit,
	}
	runSvc, err := runsvc.NewPipelineRunServiceWithOptions(deps.runRepo, deps.taskRepo,
		deps.pipelineRepo, deps.stageRepo, deps.taskTemplateRepo, deps.targetSvc,
		deps.dispatcher, deps.dispatchRepo, deps.logRepo, deps.approvalRepo,
		deps.componentMeta, opts)
	if err != nil {
		applog.Fatalf("hub: pipeline run policy invalid: %v", err)
	}
	applog.Infof("hub: pipeline run service policy: %s", runSvc.PolicySummary())

	go runSvc.SweepPending(ctx)
	go runSvc.RunApprovalTimeouts(ctx, cfg.ApprovalTimeoutInterval)
	go runSvc.RunBackground(ctx, cfg.RunSweepInterval)

	triggerSvc := pipelinesvc.NewTriggerService(deps.triggerRepo, deps.gdb, runSvc)
	finishedHook = triggerSvc.EvaluateRunFinished
	triggerSvc.StartScheduler(ctx, cfg.TriggerTickInterval)
	return runSvc, triggerSvc
}
