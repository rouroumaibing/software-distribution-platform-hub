package config

import (
	"os"
	"strconv"
	"time"

	"github.com/rouroumaibing/software-distribution-platform-hub/internal/db"
)

// Config holds every runtime tunable for the hub. Values come from env vars
// with the defaults shown below, so a local `go run ./cmd/hub` works with
// zero configuration: Postgres on localhost and OIDC auth disabled (dev).
type Config struct {
	// Addr is the host:port the HTTP + gateway server listens on.
	Addr string
	// GatewayPath is the WebSocket route Runners dial into, e.g. /gateway/ws.
	GatewayPath string
	// GatewayToken is the shared secret Runners present as a Bearer token.
	// Empty means "accept any" — only safe for local dev.
	GatewayToken string
	// DBDSN is the Postgres connection string.
	DBDSN string
	// KeycloakIssuer is the OIDC issuer URL. Empty disables auth (dev mode).
	KeycloakIssuer string
	// KeycloakClientID is the console's public client ID, checked against the
	// token's azp claim.
	KeycloakClientID string
	// KeycloakAdminClientID is the confidential client the hub uses as a
	// service account to create org carrier groups (/org:<slug>) in Keycloak.
	// Defaults to "sdp-backend" when a secret is configured.
	KeycloakAdminClientID string
	// KeycloakAdminClientSecret is the sdp-backend client secret. Empty disables
	// group provisioning (dev mode): the hub stays a passive Resource Server.
	KeycloakAdminClientSecret string
	// LogLevel is the GORM log verbosity: silent / error / warn / info.
	LogLevel string
	// BindingReapInterval is how often the expired-grant reaper runs
	// (ACCOUNT-PERMISSION-MODEL §7.4). Zero/negative disables the loop.
	BindingReapInterval time.Duration
	// ApprovalTimeoutInterval is how often pending approvals are checked
	// against their task's declared timeout and auto-rejected (B-11 审批超时).
	// 默认 60s：这是**行为正确性**（用户故事要求"超时后自动失败，而不是无限期
	// 挂起"），不是可选的运维优化，故默认开启；≤0 关闭。
	ApprovalTimeoutInterval time.Duration

	// DB pool tuning (optional; db.Open applies sane defaults when zero).
	DBMaxIdleConns    int
	DBMaxOpenConns    int
	DBConnMaxLifetime time.Duration

	// PprofEnabled mounts net/http/pprof handlers under /debug/pprof on the
	// same engine. Off by default so profiling endpoints are never exposed in
	// production unless explicitly enabled.
	PprofEnabled bool

	// SwaggerEnabled mounts the Swagger UI at /swagger/index.html. Off by
	// default to avoid publishing API docs in production.
	SwaggerEnabled bool

	// --- artifact object storage (G5) ---------------------------------------
	// ArtifactStoreDriver selects the backend: "s3" (any S3-compatible store
	// incl. MinIO), "local" (dev-only disk backend served by the hub), or ""
	// (unconfigured — artifact download/upload URL generation errors out).
	ArtifactStoreDriver string
	// S3 driver settings (endpoint like "minio.infra:9000").
	ArtifactStoreEndpoint   string
	ArtifactStoreAccessKey  string
	ArtifactStoreSecretKey  string
	ArtifactStoreSessionTok string
	ArtifactStoreBucket     string
	ArtifactStoreRegion     string
	ArtifactStoreSecure     bool
	// Local driver settings: root dir + the base URL minted into signed links
	// (must be reachable from browsers, i.e. the hub's public address).
	ArtifactStoreLocalRoot string
	ArtifactStorePublicURL string
	// SignSecret authenticates local-driver URLs; falls back to GatewayToken.
	ArtifactStoreSignSecret string
	// Presigned/signed URL lifetime.
	ArtifactStoreURLExpiry time.Duration
	// ArtifactReconcileInterval is how often the report-only orphan
	// reconciliation runs (backlog B-16 对账). **Zero disables it** — the job
	// lists the object store, whose cost grows with the bucket, so it is
	// opt-in (set ARTIFACT_RECONCILE_INTERVAL=86400 for a daily pass).
	ArtifactReconcileInterval time.Duration
	// ArtifactReconcilePrefix scopes the object listing (e.g. "components/").
	// Empty means the whole store.
	ArtifactReconcilePrefix string
	// ArtifactGCInterval is how often the retention garbage collector runs
	// (backlog B-16 收口：让 `expires_at` 真正生效). **Zero disables it** —
	// 一个会删数据的作业不该由一次部署悄悄打开（set ARTIFACT_GC_INTERVAL=3600
	// for an hourly pass）。
	ArtifactGCInterval time.Duration

	// JobServiceAccount (G-1, env SDP_JOB_SERVICE_ACCOUNT) is the
	// ServiceAccount name injected into every dispatched PipelineRunSpec so
	// execution Jobs run with deploy permissions instead of the namespace's
	// default SA (which cannot deploy). The runner side idempotently creates
	// this SA + a least-privilege deploy Role/RoleBinding in the run
	// namespace before creating Jobs. Empty disables injection (legacy
	// behavior: Jobs run as the namespace default SA).
	JobServiceAccount string
	// ArtifactGCBatch bounds how many expired rows one GC pass touches. Zero
	// falls back to service.DefaultArtifactGCBatch. The bound matters because
	// each row costs one blocking object-store call.
	ArtifactGCBatch int

	// --- package version matrix (§9.10) -----------------------------------
	// Read from the package-versions ConfigMap (hub chart renders it from
	// build/hub/versions.yaml). Surfaced by GET /package-versions so the
	// console can show the deployed component versions and the runner
	// install/upgrade flow can pick a target version. Dev mode (no CM) falls
	// back to "dev".
	PackageVersionConsole string
	PackageVersionHub     string
	PackageVersionRunner  string
}

func getenv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func getenvInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

func getenvBool(key string, def bool) bool {
	if v := os.Getenv(key); v != "" {
		return v == "true" || v == "1" || v == "yes"
	}
	return def
}

// Load reads configuration from the environment.
func Load() *Config {
	return &Config{
		Addr:                      getenv("HUB_ADDR", ":8080"),
		GatewayPath:               getenv("GATEWAY_PATH", "/gateway/ws"),
		GatewayToken:              os.Getenv("GATEWAY_TOKEN"),
		DBDSN:                     getenv("DB_DSN", "postgres://sdp:sdp@localhost:5432/sdp?sslmode=disable"),
		KeycloakIssuer:            os.Getenv("KEYCLOAK_ISSUER"),
		KeycloakClientID:          getenv("KEYCLOAK_CLIENT_ID", "sdp-console"),
		KeycloakAdminClientID:     getenv("KEYCLOAK_ADMIN_CLIENT_ID", "sdp-backend"),
		KeycloakAdminClientSecret: os.Getenv("KEYCLOAK_ADMIN_CLIENT_SECRET"),
		LogLevel:                  getenv("LOG_LEVEL", "warn"),
		DBMaxIdleConns:            getenvInt("DB_MAX_IDLE_CONNS", 10),
		DBMaxOpenConns:            getenvInt("DB_MAX_OPEN_CONNS", 100),
		DBConnMaxLifetime:         time.Duration(getenvInt("DB_CONN_MAX_LIFETIME", 3600)) * time.Second,
		BindingReapInterval:       time.Duration(getenvInt("BINDING_REAP_INTERVAL", 3600)) * time.Second,
		// 60s 一轮：审批超时的粒度是分钟级，扫得比这更勤没有收益（每轮只是一次
		// "取 Pending 行"的索引查询）。
		ApprovalTimeoutInterval: time.Duration(getenvInt("APPROVAL_TIMEOUT_INTERVAL", 60)) * time.Second,
		PprofEnabled:            getenvBool("HUB_PPROF_ENABLED", false),
		SwaggerEnabled:          getenvBool("HUB_SWAGGER_ENABLED", false),

		ArtifactStoreDriver:     os.Getenv("ARTIFACT_STORE_DRIVER"),
		ArtifactStoreEndpoint:   os.Getenv("ARTIFACT_STORE_ENDPOINT"),
		ArtifactStoreAccessKey:  os.Getenv("ARTIFACT_STORE_ACCESS_KEY"),
		ArtifactStoreSecretKey:  os.Getenv("ARTIFACT_STORE_SECRET_KEY"),
		ArtifactStoreSessionTok: os.Getenv("ARTIFACT_STORE_SESSION_TOKEN"),
		ArtifactStoreBucket:     os.Getenv("ARTIFACT_STORE_BUCKET"),
		ArtifactStoreRegion:     os.Getenv("ARTIFACT_STORE_REGION"),
		ArtifactStoreSecure:     getenvBool("ARTIFACT_STORE_SECURE", false),
		ArtifactStoreLocalRoot:  getenv("ARTIFACT_STORE_LOCAL_ROOT", "./data/artifacts"),
		ArtifactStorePublicURL:  getenv("ARTIFACT_STORE_PUBLIC_URL", "http://localhost:8080"),
		ArtifactStoreSignSecret: os.Getenv("ARTIFACT_STORE_SIGN_SECRET"),
		ArtifactStoreURLExpiry:  time.Duration(getenvInt("ARTIFACT_STORE_URL_EXPIRY", 900)) * time.Second,
		// 默认 0 = 关闭（见字段注释）：对账要列对象存储，成本随桶增长，显式开启。
		ArtifactReconcileInterval: time.Duration(getenvInt("ARTIFACT_RECONCILE_INTERVAL", 0)) * time.Second,
		ArtifactReconcilePrefix:   os.Getenv("ARTIFACT_RECONCILE_PREFIX"),
		// 默认 0 = 关闭：保留期 GC 会**删**制品，必须显式开启。
		ArtifactGCInterval: time.Duration(getenvInt("ARTIFACT_GC_INTERVAL", 0)) * time.Second,
		ArtifactGCBatch:    getenvInt("ARTIFACT_GC_BATCH", 0),

		// G-1：注入执行 Job 的 SA 名；runner 会在运行命名空间幂等 ensure
		// 该 SA + 最小部署 Role/RoleBinding。置空则回到旧行为（default SA）。
		JobServiceAccount: getenv("SDP_JOB_SERVICE_ACCOUNT", "sdp-deploy"),

		// Package version matrix (§9.10): defaults to "dev" when the CM is not
		// mounted (local `go run`, or a deploy that hasn't rendered it). The
		// deployed hub always gets these from the package-versions ConfigMap.
		PackageVersionConsole: getenv("PACKAGE_VERSION_CONSOLE", "dev"),
		PackageVersionHub:     getenv("PACKAGE_VERSION_HUB", "dev"),
		PackageVersionRunner:  getenv("PACKAGE_VERSION_RUNNER", "dev"),
	}
}

// DBConfig builds the structured database config consumed by db.Open, folding
// the env-based pool tuning into the DSN-based connection config.
func (c *Config) DBConfig() db.Config {
	return db.Config{
		DSN:             c.DBDSN,
		LogLevel:        c.LogLevel,
		MaxIdleConns:    c.DBMaxIdleConns,
		MaxOpenConns:    c.DBMaxOpenConns,
		ConnMaxLifetime: c.DBConnMaxLifetime,
	}
}

// AuthDisabled reports whether OIDC verification should be skipped. True when
// no KEYCLOAK_ISSUER is set, so the hub boots without a Keycloak realm.
func (c *Config) AuthDisabled() bool { return c.KeycloakIssuer == "" }
