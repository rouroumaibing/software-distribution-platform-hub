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
	// LogLevel is the GORM log verbosity: silent / error / warn / info.
	LogLevel string

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
		Addr:              getenv("HUB_ADDR", ":8080"),
		GatewayPath:       getenv("GATEWAY_PATH", "/gateway/ws"),
		GatewayToken:      os.Getenv("GATEWAY_TOKEN"),
		DBDSN:             getenv("DB_DSN", "postgres://sdp:sdp@localhost:5432/sdp?sslmode=disable"),
		KeycloakIssuer:    os.Getenv("KEYCLOAK_ISSUER"),
		KeycloakClientID:  getenv("KEYCLOAK_CLIENT_ID", "sdp-console"),
		LogLevel:          getenv("LOG_LEVEL", "warn"),
		DBMaxIdleConns:    getenvInt("DB_MAX_IDLE_CONNS", 10),
		DBMaxOpenConns:    getenvInt("DB_MAX_OPEN_CONNS", 100),
		DBConnMaxLifetime: time.Duration(getenvInt("DB_CONN_MAX_LIFETIME", 3600)) * time.Second,
		PprofEnabled:      getenvBool("HUB_PPROF_ENABLED", false),
		SwaggerEnabled:    getenvBool("HUB_SWAGGER_ENABLED", false),

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
