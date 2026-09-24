// Command hub is the control plane for the software distribution platform:
// a Gin HTTP API plus a WebSocket gateway that Runners dial into. It wires
// the DB, all domain services/handlers, OIDC auth (optional in dev), and the
// gateway, then serves /api/v1 and the gateway path.
package main

import (
	"context"
	"net/http"
	_ "net/http/pprof" // registers pprof handlers on http.DefaultServeMux
	"os"
	"os/signal"
	"runtime"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	_ "github.com/rouroumaibing/software-distribution-platform-hub/docs"
	"github.com/rouroumaibing/software-distribution-platform-hub/internal/metrics"
	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"

	applog "github.com/rouroumaibing/software-distribution-platform-hub/internal/common/logger"
	runnerapi "github.com/rouroumaibing/software-distribution-platform-runner/api/v1alpha1"

	artifacthandler "github.com/rouroumaibing/software-distribution-platform-hub/internal/artifact/handler"
	artifactrepo "github.com/rouroumaibing/software-distribution-platform-hub/internal/artifact/repository"
	artifactsvc "github.com/rouroumaibing/software-distribution-platform-hub/internal/artifact/service"
	artifactstorage "github.com/rouroumaibing/software-distribution-platform-hub/internal/artifact/storage"
	"github.com/rouroumaibing/software-distribution-platform-hub/internal/cascade"
	cataloghandler "github.com/rouroumaibing/software-distribution-platform-hub/internal/catalog/handler"
	catalogrepo "github.com/rouroumaibing/software-distribution-platform-hub/internal/catalog/repository"
	catalogsvc "github.com/rouroumaibing/software-distribution-platform-hub/internal/catalog/service"
	componenthandler "github.com/rouroumaibing/software-distribution-platform-hub/internal/component/handler"
	componentrepo "github.com/rouroumaibing/software-distribution-platform-hub/internal/component/repository"
	componentsvc "github.com/rouroumaibing/software-distribution-platform-hub/internal/component/service"
	config "github.com/rouroumaibing/software-distribution-platform-hub/internal/config"
	credhandler "github.com/rouroumaibing/software-distribution-platform-hub/internal/credentials/handler"
	credrepo "github.com/rouroumaibing/software-distribution-platform-hub/internal/credentials/repository"
	credsvc "github.com/rouroumaibing/software-distribution-platform-hub/internal/credentials/service"
	"github.com/rouroumaibing/software-distribution-platform-hub/internal/db"
	envhandler "github.com/rouroumaibing/software-distribution-platform-hub/internal/environment/handler"
	envrepo "github.com/rouroumaibing/software-distribution-platform-hub/internal/environment/repository"
	envsvc "github.com/rouroumaibing/software-distribution-platform-hub/internal/environment/service"
	envgrouphandler "github.com/rouroumaibing/software-distribution-platform-hub/internal/environmentgroup/handler"
	envgrouprepo "github.com/rouroumaibing/software-distribution-platform-hub/internal/environmentgroup/repository"
	envgroupsvc "github.com/rouroumaibing/software-distribution-platform-hub/internal/environmentgroup/service"
	"github.com/rouroumaibing/software-distribution-platform-hub/internal/gateway"
	"github.com/rouroumaibing/software-distribution-platform-hub/internal/keycloak"
	"github.com/rouroumaibing/software-distribution-platform-hub/internal/middleware"
	orghandler "github.com/rouroumaibing/software-distribution-platform-hub/internal/org/handler"
	orgrepo "github.com/rouroumaibing/software-distribution-platform-hub/internal/org/repository"
	orgsvc "github.com/rouroumaibing/software-distribution-platform-hub/internal/org/service"
	"github.com/rouroumaibing/software-distribution-platform-hub/internal/packageversion"
	permhandler "github.com/rouroumaibing/software-distribution-platform-hub/internal/permission/handler"
	permmodels "github.com/rouroumaibing/software-distribution-platform-hub/internal/permission/models"
	permbrepo "github.com/rouroumaibing/software-distribution-platform-hub/internal/permission/repository"
	permissionsvc "github.com/rouroumaibing/software-distribution-platform-hub/internal/permission/service"
	pipelinehandler "github.com/rouroumaibing/software-distribution-platform-hub/internal/pipeline/handler"
	pipelinerepo "github.com/rouroumaibing/software-distribution-platform-hub/internal/pipeline/repository"
	pipelinesvc "github.com/rouroumaibing/software-distribution-platform-hub/internal/pipeline/service"
	runhandler "github.com/rouroumaibing/software-distribution-platform-hub/internal/run/handler"
	runrepo "github.com/rouroumaibing/software-distribution-platform-hub/internal/run/repository"
	runsvc "github.com/rouroumaibing/software-distribution-platform-hub/internal/run/service"
	searchhandler "github.com/rouroumaibing/software-distribution-platform-hub/internal/search/handler"
	searchrepo "github.com/rouroumaibing/software-distribution-platform-hub/internal/search/repository"
	searchsvc "github.com/rouroumaibing/software-distribution-platform-hub/internal/search/service"
	targethandler "github.com/rouroumaibing/software-distribution-platform-hub/internal/target/handler"
	targetrepo "github.com/rouroumaibing/software-distribution-platform-hub/internal/target/repository"
	targetsvc "github.com/rouroumaibing/software-distribution-platform-hub/internal/target/service"
)

// @title           Software Distribution Platform Hub API
// @version         1.0
// @description     Control plane API for the K8s software distribution platform: orgs, catalogs, components, pipelines, runs, targets and permissions, plus a Runner WebSocket gateway.
// @termsOfService  http://swagger.io/terms/
// @contact.name    Platform Team
// @host            localhost:8080
// @BasePath        /api/v1
// @securityDefinitions.apikey  BearerAuth
// @in              header
// @name            Authorization
func main() {
	applog.Init()
	defer applog.Flush()

	cfg := config.Load()

	gdb, err := db.Open(cfg.DBConfig())
	if err != nil {
		applog.Fatalf("hub: failed to open db: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// --- repositories -------------------------------------------------------
	orgRepo := orgrepo.NewOrgRepository(gdb)
	serviceTreeRepo := orgrepo.NewServiceTreeRepository(gdb)
	catalogRepo := catalogrepo.NewServiceRepository(gdb)
	componentRepo := componentrepo.NewComponentRepository(gdb)
	componentConfigRepo := componentrepo.NewComponentConfigRepository(gdb)
	targetRepo := targetrepo.NewTargetRepository(gdb)
	envRepo := envrepo.NewEnvironmentRepository(gdb)
	credRepo := credrepo.NewCredentialRepository(gdb)
	envGroupRepo := envgrouprepo.NewEnvironmentGroupRepository(gdb)
	pipelineRepo := pipelinerepo.NewPipelineRepository(gdb)
	versionRepo := pipelinerepo.NewVersionRepository(gdb)
	stageRepo := pipelinerepo.NewStageRepository(gdb)
	taskTemplateRepo := pipelinerepo.NewTaskTemplateRepository(gdb)
	artifactRepo := artifactrepo.NewArtifactRepository(gdb)
	pipelineRunRepo := runrepo.NewPipelineRunRepository(gdb)
	taskRunRepo := runrepo.NewTaskRunRepository(gdb)
	taskRunLogRepo := runrepo.NewTaskRunLogRepository(gdb)
	dispatchJobRepo := runrepo.NewDispatchJobRepository(gdb)
	rolloutRunRepo := runrepo.NewRolloutRunRepository(gdb)
	roleRepo := permbrepo.NewRoleRepository(gdb)
	bindingRepo := permbrepo.NewBindingRepository(gdb)
	componentRoleRepo := permbrepo.NewComponentRoleRepository(gdb)
	platformRoleRepo := permbrepo.NewPlatformRoleRepository(gdb)
	platformBindRepo := permbrepo.NewPlatformRoleBindingRepository(gdb)
	searchRepo := searchrepo.NewSearchRepository(gdb)
	resourceOwnershipRepo := permbrepo.NewResourceOwnershipRepository(gdb)
	roleMappingRepo := permbrepo.NewRoleAPIMappingRepository(gdb)
	auditRepo := permbrepo.NewAuditLogRepository(gdb)
	permRequestRepo := permbrepo.NewPermissionRequestRepository(gdb)

	// --- services -----------------------------------------------------------
	roleSvc := permissionsvc.NewRoleService(roleRepo)
	// roleMappingSvc is the reviewable role→action registry
	// (ACCOUNT-PERMISSION-MODEL §5.1③); it is wired into bindingSvc so the
	// mapping table is a consulted source of actions, not a decorative mirror.
	roleMappingSvc := permissionsvc.NewRoleAPIMappingService(roleMappingRepo, platformRoleRepo, componentRoleRepo)
	bindingSvc := permissionsvc.NewBindingService(bindingRepo, componentRoleRepo, platformRoleRepo, platformBindRepo, componentRepo, roleMappingSvc)
	// C-10：平台级角色的定义与授予在补这两个服务之前只能靠 seed SQL 写入，
	// 没有任何主体能经 API 拿到平台级权限（ACCOUNT-PERMISSION-MODEL §10 #15）。
	platformRoleSvc := permissionsvc.NewPlatformRoleService(platformRoleRepo)
	// B-11 自定义角色（组件级）：在此之前 component_roles 只有 List，自定义角色
	// 无法经 API 建立，只能靠 seed SQL —— 组织级权限颗粒度适配不了。
	componentRoleSvc := permissionsvc.NewComponentRoleService(componentRoleRepo)
	platformBindingSvc := permissionsvc.NewPlatformBindingService(platformBindRepo, platformRoleRepo)
	resourceOwnershipSvc := permissionsvc.NewResourceOwnershipService(resourceOwnershipRepo)
	auditReporter := permissionsvc.NewAuditReporter(auditRepo)
	// permRequestSvc approves a request by writing the matching binding (with
	// expires_at). The writer routes platform vs component grants.
	permRequestSvc := permissionsvc.NewPermissionRequestService(permRequestRepo,
		permissionsvc.NewGrantWriter(bindingRepo, platformBindRepo))
	// The reaper physically removes expired grants (§7.4); authorization
	// already ignores them — this keeps the two binding tables from growing
	// without bound.
	reaper := permissionsvc.NewBindingReaper(bindingRepo, platformBindRepo)
	// Keep the reviewable role→action registry in lock-step with the role
	// definitions. Best-effort: a failure must not stop the hub booting.
	if serr := roleMappingSvc.SyncFromRoles(); serr != nil {
		applog.Warnf("hub: role→action registry sync failed (continuing): %v", serr)
	}
	// Org carrier groups (/org:<slug>) are provisioned in Keycloak when auth
	// is enabled and the sdp-backend service-account secret is configured.
	// Without it the hub stays a passive Resource Server and skips group sync
	// (dev mode). The provisioner is best-effort everywhere it is used, so a
	// Keycloak outage never blocks org creation or hub startup.
	var orgGroupProvisioner keycloak.GroupProvisioner
	if !cfg.AuthDisabled() {
		if cfg.KeycloakAdminClientSecret == "" {
			applog.Warnf("hub: KEYCLOAK_ADMIN_CLIENT_SECRET not set — org carrier groups will not be synced to Keycloak")
		} else {
			orgGroupProvisioner = keycloak.NewClient(cfg.KeycloakIssuer, cfg.KeycloakAdminClientID, cfg.KeycloakAdminClientSecret)
		}
	}
	orgSvc := orgsvc.NewOrgService(orgRepo, serviceTreeRepo, orgGroupProvisioner)
	// searchSvc 是跨资源的**导航**搜索：服务树页与 ⌘K 浮层共用同一个端点
	// （CONSOLE-UI-DESIGN.md 附 A N-8）。它无状态、无写路径，只做跨表查询。
	searchSvc := searchsvc.NewSearchService(searchRepo)
	// serviceTreeRepo 同时满足 org 侧（建树）与 catalog 侧的窄接口
	// ServiceTreeLookup（GET /orgs/:id/services，附 A N-9）。
	// cascadeDel 是域内级联删除器（DELETE-CONTRACT §6.4）：catalog/component
	// 两处 Delete 走它，在**同一事务**内清理子资源 —— 这是 §1.3 记的
	// 「校验与删除不在同一事务」缺口的落地（活跃运行判定仍在各 service）。
	cascadeDel := cascade.NewDeleter(gdb)
	catalogSvc := catalogsvc.NewServiceService(catalogRepo, pipelineRunRepo, serviceTreeRepo).WithCascade(cascadeDel)
	componentSvc := componentsvc.NewComponentService(componentRepo, bindingRepo, componentRoleRepo, pipelineRunRepo).WithCascade(cascadeDel)
	componentConfigSvc := componentsvc.NewComponentConfigService(componentConfigRepo, envRepo)
	targetSvc := targetsvc.NewTargetService(targetRepo)
	// §9.9 / §9.5 agent operation ledger（exec / install / upgrade）：hub 负责
	// 校验 + 记账 + 出 202 句柄，执行归 Runner（hub 无 client-go）。
	agentOpSvc := targetsvc.NewAgentOpService(targetrepo.NewAgentOpRepository(gdb))
	envSvc := envsvc.NewEnvironmentService(envRepo, targetRepo, componentConfigRepo)
	envSvc.SetAgentOpStore(agentOpSvc)
	credSvc := credsvc.NewCredentialService(credRepo)
	envGroupSvc := envgroupsvc.NewEnvironmentGroupService(envGroupRepo)
	// versionSvc 是流水线定义历史的**唯一写者**：pipelines.version 的自增与
	// pipeline_versions 的留档都在这里，别的服务只能委托它（C-09）。
	versionSvc := pipelinesvc.NewPipelineVersionService(pipelineRepo, stageRepo, taskTemplateRepo, versionRepo)
	pipelineSvc := pipelinesvc.NewPipelineService(pipelineRepo, versionSvc, pipelineRunRepo)
	// stageSvc 拿 pipelineRepo 做父存在性校验、拿 taskTemplateRepo 做软删级联
	//（DDL 的 on delete cascade 对软删不触发）；taskTemplateSvc 复用 stageSvc，
	// 因为任务模板的父链是 task → stage → pipeline（见 service/stage.go）。
	stageSvc := pipelinesvc.NewStageService(stageRepo, pipelineRepo, taskTemplateRepo)
	taskTemplateSvc := pipelinesvc.NewTaskTemplateService(taskTemplateRepo, stageSvc)
	// C-09：结构改动必须留档 —— 把版本发布钩子接到两个结构服务上，于是"建/改/删
	// 阶段或子任务"都会重新快照整份定义（体未变则去重，不产生空版本）。
	stageSvc.SetVersionPublisher(versionSvc)
	taskTemplateSvc.SetVersionPublisher(versionSvc)

	// --- artifact object storage (G5) ----------------------------------------
	// Driver comes from ARTIFACT_STORE_DRIVER: s3 (MinIO/S3/OSS) or local
	// (dev-only disk backend, served by the hub with HMAC-signed URLs).
	// Unconfigured → nil store: metadata keeps working, URL minting 503s.
	var artifactStore artifactstorage.Client
	var localArtifactStore *artifactstorage.Local
	switch cfg.ArtifactStoreDriver {
	case "s3":
		s3Store, err := artifactstorage.NewS3(artifactstorage.S3Config{
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
		artifactStore = s3Store
		applog.Infof("hub: artifact storage driver=s3 bucket=%s endpoint=%s", cfg.ArtifactStoreBucket, cfg.ArtifactStoreEndpoint)
	case "local":
		secret := cfg.ArtifactStoreSignSecret
		if secret == "" {
			secret = cfg.GatewayToken
		}
		if secret == "" {
			applog.Fatalf("hub: local artifact storage needs ARTIFACT_STORE_SIGN_SECRET or GATEWAY_TOKEN to sign URLs")
		}
		localStore, err := artifactstorage.NewLocal(cfg.ArtifactStoreLocalRoot, secret, cfg.ArtifactStorePublicURL)
		if err != nil {
			applog.Fatalf("hub: cannot init local artifact storage at %s: %v", cfg.ArtifactStoreLocalRoot, err)
		}
		artifactStore = localStore
		applog.Infof("hub: artifact storage driver=local root=%s public=%s", cfg.ArtifactStoreLocalRoot, cfg.ArtifactStorePublicURL)
	default:
		if cfg.ArtifactStoreDriver != "" {
			applog.Fatalf("hub: unknown ARTIFACT_STORE_DRIVER %q (want s3|local)", cfg.ArtifactStoreDriver)
		}
		applog.Info("hub: artifact storage not configured — downloads/uploads will 503 until ARTIFACT_STORE_DRIVER is set")
	}

	artifactSvc := artifactsvc.NewArtifactService(artifactRepo, artifactStore, cfg.ArtifactStoreURLExpiry)

	// --- artifact orphan reconciliation (B-16 对账，仅报告) -------------------
	// 只在**驱动支持列举**时装配：不支持时如实跳过，而不是报出"0 个孤儿"
	// （那会把"查不了"伪装成"很干净"）。默认关闭（interval=0），见 config 注释。
	if enumerator, ok := artifactStore.(artifactstorage.Enumerator); ok {
		reconciler := artifactsvc.NewArtifactReconciler(artifactRepo, enumerator, cfg.ArtifactReconcilePrefix)
		go reconciler.Run(ctx, cfg.ArtifactReconcileInterval)
		if cfg.ArtifactReconcileInterval > 0 {
			applog.Infof("hub: artifact orphan reconciliation enabled every %s (prefix=%q, report-only)",
				cfg.ArtifactReconcileInterval, cfg.ArtifactReconcilePrefix)
		}
	} else if artifactStore != nil {
		applog.Warnf("hub: artifact storage driver does not support listing — orphan reconciliation (B-16) is skipped, not reported as clean")
	}

	// --- artifact retention GC (B-16 收口：让 `expires_at` 真正生效) -----------
	// 与对账**分开**装配：对账只报告（输入有合法歧义），GC 会删（输入是运维自己写下的
	// 保留期声明）。默认关闭（interval=0），显式设置 ARTIFACT_GC_INTERVAL 才启用。
	//
	// typed-nil 处理：把一个 storage.Client(nil) 直接转成 ObjectDeleter 会得到一个
	// **非 nil** 的接口（底层指针为 nil），GC 里的 `store != nil` 会误判成"已配置"并在
	// Delete 上 panic。所以只在本进程真的建出了驱动时才赋值。
	var gcDeleter artifactsvc.ObjectDeleter
	if artifactStore != nil {
		gcDeleter = artifactStore
	}
	artifactGC := artifactsvc.NewArtifactGC(artifactRepo, gcDeleter, cfg.ArtifactGCBatch)
	go artifactGC.Run(ctx, cfg.ArtifactGCInterval)
	if cfg.ArtifactGCInterval > 0 {
		gcBatch := cfg.ArtifactGCBatch
		if gcBatch <= 0 {
			gcBatch = artifactsvc.DefaultArtifactGCBatch
		}
		applog.Infof("hub: artifact retention GC enabled every %s (batch=%d, objectStore=%v)",
			cfg.ArtifactGCInterval, gcBatch, gcDeleter != nil)
	}

	// --- gateway (Hub side of the Runner long connection) -------------------
	gw := gateway.New(targetSvc, cfg.GatewayToken)

	// --- agent op 全链路（§9.5 / §9.9，UNIMPLEMENTED-MODULES-PLAN §16.5）------
	// 派发器把 queued 的 exec op 组装成 wire payload 推到目标 runner 连接上
	// （kubeconfig-access 环境在这里解密凭据随 payload 下发）；install/upgrade
	// 留守 queued（bootstrap 流程属独立特性，见 dispatcher 文件头裁定）。
	// opStream 是 SSE 扇出源：状态/日志事件由下方两个 gateway 回调驱动。
	opStream := targetsvc.NewOpStream()
	agentOpSvc.SetDispatcher(&agentOpDispatcher{gw: gw, envRepo: envRepo, credRepo: credRepo})
	agentOpSvc.SetStream(opStream)
	gw.SetAgentOpStatusHandler(func(ctx context.Context, targetID uuid.UUID, payload *runnerapi.AgentOpStatusPayload) {
		opID, err := uuid.Parse(payload.OpID)
		if err != nil {
			applog.Infof("gateway: agent op status bad op id %q: %v", payload.OpID, err)
			return
		}
		if _, err := agentOpSvc.ApplyStatus(ctx, opID, payload.Status, payload.Message); err != nil {
			applog.Infof("gateway: agent op status apply failed (op %s → %s): %v", payload.OpID, payload.Status, err)
		}
	})
	gw.SetAgentOpLogHandler(func(ctx context.Context, targetID uuid.UUID, payload *runnerapi.AgentOpLogPayload) {
		opID, err := uuid.Parse(payload.OpID)
		if err != nil {
			applog.Infof("gateway: agent op log bad op id %q: %v", payload.OpID, err)
			return
		}
		if _, err := agentOpSvc.AppendLog(ctx, opID, payload.Stream, payload.Chunk); err != nil {
			applog.Infof("gateway: agent op log append failed (op %s): %v", payload.OpID, err)
		}
	})

	// run service needs the gateway to dispatch work; the gateway calls back
	// into the run service when Runner status arrives. Wired here to avoid a
	// package cycle (run defines a Dispatcher interface, gateway implements it).
	approvalRepo := runrepo.NewPipelineApprovalRepository(gdb)
	// componentMetaResolver lets the run service denormalize org_id /
	// component_id onto hub-side PipelineApproval rows (DATA-MODEL §7.4).
	componentMeta := &componentMetaResolver{pipelineRepo: pipelineRepo, componentRepo: componentRepo}
	// 第三个参数注入 pipeline SERVICE（而非裸 repo）：run 触发路径改走
	// pipeline service 的存活校验 (GetByID 软删感知)，不绕开领域层
	// (run 触发路径未补齐项 #1)。pipelineSvc 满足 run 服务的 PipelineDefStore
	// 窄接口（GetByID）。
	runSvc := runsvc.NewPipelineRunService(pipelineRunRepo, taskRunRepo, pipelineSvc, stageRepo, taskTemplateRepo, targetSvc, gw, dispatchJobRepo, taskRunLogRepo, approvalRepo, componentMeta)
	gw.SetStatusHandler(func(ctx context.Context, targetID uuid.UUID, payload *runnerapi.StatusUpdatePayload) {
		_ = runSvc.ApplyStatus(ctx, targetID, payload)
	})
	gw.SetLogHandler(func(ctx context.Context, payload *runnerapi.LogChunkPayload) {
		if payload.TargetID == "" {
			return
		}
		targetID, err := uuid.Parse(payload.TargetID)
		if err != nil {
			applog.Infof("gateway: log chunk bad target id %q: %v", payload.TargetID, err)
			return
		}
		// Persist the chunk so the console's G2 log-read endpoint can replay
		// it even after the Runner Pod is gone. A failure here is logged but
		// does not break the gateway read loop.
		if err := runSvc.RecordLogChunk(ctx, targetID, payload); err != nil {
			applog.Infof("gateway: persist log chunk failed for run %s: %v", payload.PipelineRunName, err)
		}
	})
	// When a Runner (re)connects, redeliver any dispatch jobs enqueued while
	// it was offline so those runs are not lost. The agent-op ledger gets the
	// same drain: exec ops queued while the target was offline are pushed now
	// (§9.5 / §16.5); install/upgrade stay queued by design (bootstrap flow).
	gw.SetConnectHandler(func(ctx context.Context, targetID uuid.UUID) {
		if err := runSvc.DrainTarget(ctx, targetID); err != nil {
			applog.Infof("gateway: drain failed for target %s: %v", targetID, err)
		}
		if err := agentOpSvc.DrainTarget(ctx, targetID); err != nil {
			applog.Infof("gateway: agent op drain failed for target %s: %v", targetID, err)
		}
	})
	// Background sweeper retries failed dispatch jobs on backoff. Runs until
	// the app context is cancelled at shutdown.
	go runSvc.SweepPending(ctx)
	// Background reaper physically deletes expired role bindings (§7.4).
	go reaper.Run(ctx, cfg.BindingReapInterval)
	// B-11 生产强审批：向生产环境触发时，流水线必须带人工审核阶段。
	// envRepo 同时满足窄接口 ProductionPolicy（只暴露一次批量查询）。
	runSvc.SetProductionPolicy(envRepo)
	// B-11 审批超时：把超过任务时限仍未决策的审批自动拒绝（用户故事 Epic 8）。
	go runSvc.RunApprovalTimeouts(ctx, cfg.ApprovalTimeoutInterval)

	// --- handlers -----------------------------------------------------------
	orgHandler := orghandler.NewOrgHandler(orgSvc)
	catalogHandler := cataloghandler.NewServiceHandler(catalogSvc)
	componentHandler := componenthandler.NewComponentHandler(componentSvc)
	componentConfigHandler := componenthandler.NewComponentConfigHandler(componentConfigSvc)
	targetHandler := targethandler.NewTargetHandler(targetSvc)
	// 接入编排（§9.9）：install/upgrade 取版本矩阵里的 runner 版本。
	targetHandler.SetAgentOps(agentOpSvc, cfg.PackageVersionRunner)
	// agent op 台账读侧：轮询 / 列表 / SSE 流式（§16.5）。
	agentOpHandler := targethandler.NewAgentOpHandler(agentOpSvc)
	envHandler := envhandler.NewEnvironmentHandler(envSvc)
	credHandler := credhandler.NewCredentialHandler(credSvc)
	envGroupHandler := envgrouphandler.NewEnvironmentGroupHandler(envGroupSvc)
	pipelineHandler := pipelinehandler.NewPipelineHandler(pipelineSvc)
	stageHandler := pipelinehandler.NewStageHandler(stageSvc)
	taskTemplateHandler := pipelinehandler.NewTaskTemplateHandler(taskTemplateSvc)
	pipelineVersionHandler := pipelinehandler.NewPipelineVersionHandler(versionSvc)
	artifactHandler := artifacthandler.NewArtifactHandler(artifactSvc)
	roleHandler := permhandler.NewRoleHandler(roleSvc)
	componentRoleHandler := permhandler.NewComponentRoleHandler(componentRoleSvc)
	resourceOwnershipHandler := permhandler.NewResourceOwnershipHandler(resourceOwnershipSvc)
	roleMappingHandler := permhandler.NewRoleAPIMappingHandler(roleMappingSvc, roleMappingRepo)
	permRequestHandler := permhandler.NewPermissionRequestHandler(permRequestSvc)
	userInfoHandler := permhandler.NewUserInfoHandler(platformBindRepo, platformRoleRepo)
	pipelineRunHandler := runhandler.NewPipelineRunHandler(runSvc)
	searchHandler := searchhandler.NewSearchHandler(searchSvc)
	releaseSvc := runsvc.NewReleaseService(rolloutRunRepo)
	releaseHandler := runhandler.NewReleaseHandler(releaseSvc)
	pkgVerHandler := packageversion.NewHandler(cfg)

	// --- auth (optional) ----------------------------------------------------
	var auth *middleware.Authenticator
	if cfg.AuthDisabled() {
		applog.Info("hub: auth disabled (no KEYCLOAK_ISSUER set) — dev mode")
	} else {
		auth, err = middleware.NewAuthenticator(ctx, middleware.AuthConfig{
			IssuerURL: cfg.KeycloakIssuer,
			ClientID:  cfg.KeycloakClientID,
		})
		if err != nil {
			applog.Fatalf("hub: failed to init authenticator: %v", err)
		}
	}

	// --- router -------------------------------------------------------------
	// One-time backfill: ensure every existing org already has its `/org:<slug>`
	// carrier group in Keycloak (orgs created before group provisioning existed
	// have none). Best-effort and idempotent — a Keycloak outage is logged, not
	// fatal. Runs before the router so groups exist by the time the API serves.
	orgSvc.ReconcileGroups(ctx)

	r := gin.Default()

	// B-09: Hub-side metrics surface. The Runner already serves /metrics via
	// controller-runtime's built-in server; this gives the Hub the same
	// scrape target (hand-rolled Prometheus text, no extra dependency) so a
	// single scrape job covers both control-plane and runner. The middleware
	// records per-request counters and latency; /metrics exposes them.
	r.Use(metrics.GinMiddleware())
	metrics.SetBuildInfo("dev", "dev", runtime.Version())
	r.GET("/metrics", metrics.GinHandler())

	api := r.Group("/api/v1")

	if auth != nil {
		api.Use(auth.Middleware())
	}
	// UserContext runs after auth and lifts the token's identity dimensions
	// (subject / display name / groups / orgs) into the request context. It is
	// pure — no DB, no provisioned row — because hub keeps no user table
	// (ACCOUNT-PERMISSION-MODEL §2.2 / D3).
	api.Use(middleware.UserContext())
	// AuditMiddleware is the single cross-cutting writer for mutating requests
	// (§6). It runs after UserContext so the subject is resolved, and wraps
	// every route so business code never writes audit rows itself.
	api.Use(middleware.AuditMiddleware(auditReporter))

	// Org / target / catalog / component management — no component-scoped
	// permission check in this M1 wiring.
	orgHandler.RegisterRoutes(api)
	targetHandler.RegisterRoutes(api)
	agentOpHandler.RegisterRoutes(api)
	catalogHandler.RegisterRoutes(api)
	componentHandler.RegisterRoutes(api)
	componentConfigHandler.RegisterRoutes(api)
	envHandler.RegisterRoutes(api)
	credHandler.RegisterRoutes(api)
	envGroupHandler.RegisterRoutes(api)
	pipelineHandler.RegisterRoutes(api)
	stageHandler.RegisterRoutes(api)
	taskTemplateHandler.RegisterRoutes(api)
	pipelineVersionHandler.RegisterRoutes(api)
	artifactHandler.RegisterRoutes(api)
	releaseHandler.RegisterRoutes(api)
	roleHandler.RegisterRoutes(api)
	componentRoleHandler.RegisterRoutes(api)
	bindingHandler := permhandler.NewBindingHandler(bindingSvc, componentRepo)
	bindingHandler.RegisterRoutes(api)
	// C-10 平台级 RBAC 端点 + §3 / §5.1③ / §6 / §7.2 的四张表端点。
	// §11 步骤 4 收口：开启鉴权后这些平台级路由要求持有平台级 user:manage
	// 动作（种子里 `/sdp-admin` 组 → sdp-admin 角色即含该动作）；鉴权关闭
	// （dev 模式）时仍裸挂，与其余管理面一致。
	platformGroup := api.Group("/")
	if auth != nil {
		platformGroup.Use(middleware.RequirePlatformPermission(bindingSvc, permmodels.ActionUserManage))
	}
	permhandler.NewPlatformRoleHandler(platformRoleSvc).RegisterRoutes(platformGroup)
	permhandler.NewPlatformRoleBindingHandler(platformBindingSvc).RegisterRoutes(platformGroup)
	// B-11 自定义角色：组件级角色的**读**在上面的裸 api（console 角色选择器要用），
	// **写**与平台级角色同组 —— 角色定义是权限来源，必须过平台级 user:manage。
	componentRoleHandler.RegisterAdminRoutes(platformGroup)
	resourceOwnershipHandler.RegisterRoutes(platformGroup)
	roleMappingHandler.RegisterRoutes(platformGroup)
	permRequestHandler.RegisterRoutes(platformGroup)
	userInfoHandler.RegisterRoutes(api)
	searchHandler.RegisterRoutes(api)
	// §9.10 版本矩阵（CM package-versions → env → 只读端点）。
	pkgVerHandler.RegisterRoutes(api)

	// Component-scoped routes get a RequirePermission wrapper, but only when
	// auth is enabled; in dev mode they're mounted bare. The locator resolves
	// the pipeline/run ids some of these routes carry to the component that
	// owns them, so the binding check runs against the right component
	// instead of looking a pipeline id up as if it were a component id
	// (ACCOUNT-PERMISSION-MODEL §10 #14).
	// 组件作用域路由挂两层守卫（仅开启鉴权时；dev 模式裸挂）。§4 的鉴权链是
	// 两段固定顺序，短路语义一致：
	//   a 段 资源归属（吃 token 的 `/org:<slug>`，查 resource_ownership）
	//   b 段 RBAC 决策（吃 hub 绑定表，按 token `sub`/组查绑定）
	// 两段都先经 permLocator 把路径 id 解析到**拥有它的组件**，所以流水线 /
	// 运行继承其组件的归属 —— 归属只需登记一次，不必每个 pipeline 一行。
	permLocator := permissionsvc.NewComponentLocator(pipelineRepo, pipelineRunRepo)
	wrap := func(res middleware.Resource, param, perm string, h gin.HandlerFunc) []gin.HandlerFunc {
		if auth == nil {
			return []gin.HandlerFunc{h}
		}
		req := middleware.Requirement{Resource: res, Param: param, Permission: perm}
		return []gin.HandlerFunc{
			middleware.RequireResourceOwnership(resourceOwnershipSvc, orgRepo, permLocator, req),
			middleware.RequirePermission(bindingSvc, permLocator, req),
			h,
		}
	}
	scoped := api.Group("/")
	scoped.POST("/pipelines/:id/runs", wrap(middleware.ResourcePipeline, "id", permmodels.ActionPipelineTrigger, pipelineRunHandler.Trigger)...)
	scoped.GET("/pipelines/:id/runs", wrap(middleware.ResourcePipeline, "id", permmodels.ActionComponentRead, pipelineRunHandler.ListByPipeline)...)
	// 全局运行列表（运行中心）：跨 pipeline 巡视，支持 ?phase= 过滤。
	scoped.GET("/runs", pipelineRunHandler.ListAll)
	scoped.GET("/runs/:id", pipelineRunHandler.Get)
	scoped.GET("/runs/:id/tasks", pipelineRunHandler.ListTasks)
	scoped.GET("/runs/:id/progress", pipelineRunHandler.Progress)
	// DATA-MODEL §6.5 确认新增：阶段级进度卡（derive-on-read 聚合）。
	scoped.GET("/runs/:id/stage-progress", pipelineRunHandler.StageProgress)
	scoped.GET("/runs/:id/log", pipelineRunHandler.GetLogs)
	scoped.GET("/runs/:id/tasks/:name/log", pipelineRunHandler.GetLogs)
	scoped.POST("/pipelines/:id/runs/:runId/tasks/:taskName/decision", wrap(middleware.ResourcePipeline, "id", permmodels.ActionApprovalApprove, pipelineRunHandler.Approve)...)
	scoped.POST("/runs/:id/tasks/:name/rollout", pipelineRunHandler.ControlRollout)
	// 重派发：把卡在 Pending 的 run 重新投递给当前在线的 runner。
	scoped.POST("/runs/:id/redispatch", wrap(middleware.ResourceRun, "id", permmodels.ActionPipelineTrigger, pipelineRunHandler.Redispatch)...)

	// Runner gateway WebSocket endpoint.
	r.GET(cfg.GatewayPath, gw.ServeWS)

	// Local-driver signed storage routes (G5) — mounted on the root engine,
	// outside auth middleware: browser downloads carry no Authorization
	// header, the HMAC token IS the authorization.
	if localArtifactStore != nil {
		r.Any(artifactstorage.RoutePrefixPattern(), localArtifactStore.GinHandler())
	}

	// --- optional debug / docs endpoints ----------------------------------
	// pprof (off by default) is mounted on the same engine behind a flag so
	// it is never exposed in production unless explicitly enabled.
	if cfg.PprofEnabled {
		r.Any("/debug/pprof/*any", gin.WrapH(http.DefaultServeMux))
		applog.Infof("hub: pprof enabled at %s/debug/pprof/", cfg.Addr)
	}
	// Swagger UI (off by default) documents the API; enable only in dev/staging.
	if cfg.SwaggerEnabled {
		r.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerFiles.Handler))
		applog.Infof("hub: swagger UI enabled at %s/swagger/index.html", cfg.Addr)
	}

	// --- graceful startup / shutdown --------------------------------------
	// Use an explicit http.Server so we can stop accepting new connections and
	// drain in-flight requests on SIGINT/SIGTERM instead of hard-cutting them.
	srv := &http.Server{
		Addr:    cfg.Addr,
		Handler: r,
	}

	go func() {
		applog.Infof("hub: listening on %s (gateway %s)", cfg.Addr, cfg.GatewayPath)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			applog.Fatalf("hub: server exited: %v", err)
		}
	}()

	// Wait for interrupt or terminate signal.
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	applog.Infof("hub: shutting down ...")

	// Give in-flight requests up to 10s to finish, then force close.
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		applog.Errorf("hub: forced shutdown (timeout): %v", err)
	}

	// Stop the dispatch sweeper goroutine now that the server is drained.
	cancel()

	// Release the DB pool after the HTTP server stops serving.
	if err := db.Close(gdb); err != nil {
		applog.Errorf("hub: db close error: %v", err)
	}
	applog.Infof("hub: server exiting")
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

// componentMetaResolver implements runsvc.ComponentMetaStore: it maps a
// pipeline to its owning component + org so the run service can denormalize
// org_id / component_id onto hub-side PipelineApproval rows (DATA-MODEL §7.4).
type componentMetaResolver struct {
	pipelineRepo  runsvc.PipelineDefStore
	componentRepo *componentrepo.ComponentRepository
}

func (r *componentMetaResolver) ResolveComponentOrg(pipelineID uuid.UUID) (uuid.UUID, uuid.UUID, error) {
	p, err := r.pipelineRepo.GetByID(pipelineID)
	if err != nil {
		return uuid.Nil, uuid.Nil, err
	}
	orgID, err := r.componentRepo.GetOrgID(p.ComponentID)
	if err != nil {
		return uuid.Nil, uuid.Nil, err
	}
	return p.ComponentID, orgID, nil
}
