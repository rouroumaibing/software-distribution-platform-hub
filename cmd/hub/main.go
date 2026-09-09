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
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	_ "github.com/rouroumaibing/software-distribution-platform-hub/docs"
	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"

	applog "github.com/rouroumaibing/software-distribution-platform-hub/internal/common/logger"
	runnerapi "github.com/rouroumaibing/software-distribution-platform-runner/api/v1alpha1"

	artifacthandler "github.com/rouroumaibing/software-distribution-platform-hub/internal/artifact/handler"
	artifactrepo "github.com/rouroumaibing/software-distribution-platform-hub/internal/artifact/repository"
	artifactsvc "github.com/rouroumaibing/software-distribution-platform-hub/internal/artifact/service"
	artifactstorage "github.com/rouroumaibing/software-distribution-platform-hub/internal/artifact/storage"
	cataloghandler "github.com/rouroumaibing/software-distribution-platform-hub/internal/catalog/handler"
	catalogrepo "github.com/rouroumaibing/software-distribution-platform-hub/internal/catalog/repository"
	catalogsvc "github.com/rouroumaibing/software-distribution-platform-hub/internal/catalog/service"
	clusterhandler "github.com/rouroumaibing/software-distribution-platform-hub/internal/cluster/handler"
	clusterrepo "github.com/rouroumaibing/software-distribution-platform-hub/internal/cluster/repository"
	clustersvc "github.com/rouroumaibing/software-distribution-platform-hub/internal/cluster/service"
	componenthandler "github.com/rouroumaibing/software-distribution-platform-hub/internal/component/handler"
	componentrepo "github.com/rouroumaibing/software-distribution-platform-hub/internal/component/repository"
	componentsvc "github.com/rouroumaibing/software-distribution-platform-hub/internal/component/service"
	config "github.com/rouroumaibing/software-distribution-platform-hub/internal/config"
	"github.com/rouroumaibing/software-distribution-platform-hub/internal/db"
	envhandler "github.com/rouroumaibing/software-distribution-platform-hub/internal/environment/handler"
	envrepo "github.com/rouroumaibing/software-distribution-platform-hub/internal/environment/repository"
	envsvc "github.com/rouroumaibing/software-distribution-platform-hub/internal/environment/service"
	"github.com/rouroumaibing/software-distribution-platform-hub/internal/gateway"
	"github.com/rouroumaibing/software-distribution-platform-hub/internal/middleware"
	orghandler "github.com/rouroumaibing/software-distribution-platform-hub/internal/org/handler"
	orgrepo "github.com/rouroumaibing/software-distribution-platform-hub/internal/org/repository"
	orgsvc "github.com/rouroumaibing/software-distribution-platform-hub/internal/org/service"
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
)

// @title           Software Distribution Platform Hub API
// @version         1.0
// @description     Control plane API for the K8s software distribution platform: orgs, catalogs, components, pipelines, runs, clusters and permissions, plus a Runner WebSocket gateway.
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
	clusterRepo := clusterrepo.NewClusterRepository(gdb)
	envRepo := envrepo.NewEnvironmentRepository(gdb)
	pipelineRepo := pipelinerepo.NewPipelineRepository(gdb)
	versionRepo := pipelinerepo.NewVersionRepository(gdb)
	stageRepo := pipelinerepo.NewStageRepository(gdb)
	taskTemplateRepo := pipelinerepo.NewTaskTemplateRepository(gdb)
	artifactRepo := artifactrepo.NewArtifactRepository(gdb)
	pipelineRunRepo := runrepo.NewPipelineRunRepository(gdb)
	taskRunRepo := runrepo.NewTaskRunRepository(gdb)
	taskRunLogRepo := runrepo.NewTaskRunLogRepository(gdb)
	dispatchJobRepo := runrepo.NewDispatchJobRepository(gdb)
	userRepo := permbrepo.NewUserRepository(gdb)
	roleRepo := permbrepo.NewRoleRepository(gdb)
	bindingRepo := permbrepo.NewBindingRepository(gdb)

	// --- services -----------------------------------------------------------
	userSvc := permissionsvc.NewUserService(userRepo)
	roleSvc := permissionsvc.NewRoleService(roleRepo)
	bindingSvc := permissionsvc.NewBindingService(bindingRepo, roleRepo)
	orgSvc := orgsvc.NewOrgService(orgRepo, serviceTreeRepo)
	catalogSvc := catalogsvc.NewServiceService(catalogRepo)
	componentSvc := componentsvc.NewComponentService(componentRepo)
	componentConfigSvc := componentsvc.NewComponentConfigService(componentConfigRepo)
	clusterSvc := clustersvc.NewClusterService(clusterRepo)
	envSvc := envsvc.NewEnvironmentService(envRepo)
	pipelineSvc := pipelinesvc.NewPipelineService(pipelineRepo, versionRepo, pipelineRunRepo)
	stageSvc := pipelinesvc.NewStageService(stageRepo)
	taskTemplateSvc := pipelinesvc.NewTaskTemplateService(taskTemplateRepo)

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

	// --- gateway (Hub side of the Runner long connection) -------------------
	gw := gateway.New(clusterSvc, cfg.GatewayToken)

	// run service needs the gateway to dispatch work; the gateway calls back
	// into the run service when Runner status arrives. Wired here to avoid a
	// package cycle (run defines a Dispatcher interface, gateway implements it).
	runSvc := runsvc.NewPipelineRunService(pipelineRunRepo, taskRunRepo, pipelineRepo, stageRepo, taskTemplateRepo, clusterSvc, gw, dispatchJobRepo, taskRunLogRepo)
	gw.SetStatusHandler(func(ctx context.Context, clusterID uuid.UUID, payload *runnerapi.StatusUpdatePayload) {
		_ = runSvc.ApplyStatus(ctx, clusterID, payload)
	})
	gw.SetLogHandler(func(ctx context.Context, payload *runnerapi.LogChunkPayload) {
		if payload.ClusterID == "" {
			return
		}
		clusterID, err := uuid.Parse(payload.ClusterID)
		if err != nil {
			applog.Infof("gateway: log chunk bad cluster id %q: %v", payload.ClusterID, err)
			return
		}
		// Persist the chunk so the console's G2 log-read endpoint can replay
		// it even after the Runner Pod is gone. A failure here is logged but
		// does not break the gateway read loop.
		if err := runSvc.RecordLogChunk(ctx, clusterID, payload); err != nil {
			applog.Infof("gateway: persist log chunk failed for run %s: %v", payload.PipelineRunName, err)
		}
	})
	// When a Runner (re)connects, redeliver any dispatch jobs enqueued while
	// it was offline so those runs are not lost.
	gw.SetConnectHandler(func(ctx context.Context, clusterID uuid.UUID) {
		if err := runSvc.DrainCluster(ctx, clusterID); err != nil {
			applog.Infof("gateway: drain failed for cluster %s: %v", clusterID, err)
		}
	})
	// Background sweeper retries failed dispatch jobs on backoff. Runs until
	// the app context is cancelled at shutdown.
	go runSvc.SweepPending(ctx)

	// --- handlers -----------------------------------------------------------
	orgHandler := orghandler.NewOrgHandler(orgSvc)
	catalogHandler := cataloghandler.NewServiceHandler(catalogSvc)
	componentHandler := componenthandler.NewComponentHandler(componentSvc)
	componentConfigHandler := componenthandler.NewComponentConfigHandler(componentConfigSvc)
	clusterHandler := clusterhandler.NewClusterHandler(clusterSvc)
	envHandler := envhandler.NewEnvironmentHandler(envSvc)
	pipelineHandler := pipelinehandler.NewPipelineHandler(pipelineSvc)
	stageHandler := pipelinehandler.NewStageHandler(stageSvc)
	taskTemplateHandler := pipelinehandler.NewTaskTemplateHandler(taskTemplateSvc)
	artifactHandler := artifacthandler.NewArtifactHandler(artifactSvc)
	userHandler := permhandler.NewUserHandler(userSvc)
	roleHandler := permhandler.NewRoleHandler(roleSvc)
	bindingHandler := permhandler.NewBindingHandler(bindingSvc)
	pipelineRunHandler := runhandler.NewPipelineRunHandler(runSvc)

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
	r := gin.Default()
	api := r.Group("/api/v1")

	if auth != nil {
		api.Use(auth.Middleware())
	}
	// UserContext runs after auth (or provisions a dev user when auth is off).
	api.Use(middleware.UserContext(userSvc))

	// Org / cluster / catalog / component management — no component-scoped
	// permission check in this M1 wiring.
	orgHandler.RegisterRoutes(api)
	clusterHandler.RegisterRoutes(api)
	catalogHandler.RegisterRoutes(api)
	componentHandler.RegisterRoutes(api)
	componentConfigHandler.RegisterRoutes(api)
	envHandler.RegisterRoutes(api)
	pipelineHandler.RegisterRoutes(api)
	stageHandler.RegisterRoutes(api)
	taskTemplateHandler.RegisterRoutes(api)
	artifactHandler.RegisterRoutes(api)
	userHandler.RegisterRoutes(api)
	roleHandler.RegisterRoutes(api)
	bindingHandler.RegisterRoutes(api)

	// Component-scoped routes get a RequirePermission wrapper, but only when
	// auth is enabled; in dev mode they're mounted bare.
	wrap := func(perm, param string, h gin.HandlerFunc) []gin.HandlerFunc {
		if auth == nil {
			return []gin.HandlerFunc{h}
		}
		return []gin.HandlerFunc{middleware.RequirePermission(bindingSvc, param, perm), h}
	}
	scoped := api.Group("/")
	scoped.POST("/pipelines/:id/runs", wrap(permmodels.PermissionEdit, "id", pipelineRunHandler.Trigger)...)
	scoped.GET("/pipelines/:id/runs", wrap(permmodels.PermissionView, "id", pipelineRunHandler.ListByPipeline)...)
	// 全局运行列表（运行中心）：跨 pipeline 巡视，支持 ?phase= 过滤。
	scoped.GET("/runs", pipelineRunHandler.ListAll)
	scoped.GET("/runs/:id", pipelineRunHandler.Get)
	scoped.GET("/runs/:id/tasks", pipelineRunHandler.ListTasks)
	scoped.GET("/runs/:id/progress", pipelineRunHandler.Progress)
	scoped.GET("/runs/:id/log", pipelineRunHandler.GetLogs)
	scoped.GET("/runs/:id/tasks/:name/log", pipelineRunHandler.GetLogs)
	scoped.POST("/pipelines/:id/runs/:runId/tasks/:taskName/decision", wrap(permmodels.PermissionEdit, "id", pipelineRunHandler.Approve)...)
	scoped.POST("/runs/:id/tasks/:name/rollout", pipelineRunHandler.ControlRollout)
	// 重派发：把卡在 Pending 的 run 重新投递给当前在线的 runner。
	scoped.POST("/runs/:id/redispatch", wrap(permmodels.PermissionEdit, "id", pipelineRunHandler.Redispatch)...)

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
