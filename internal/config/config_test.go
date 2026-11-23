package config

import (
	"strings"
	"testing"
)

func TestLoadDefaultsAndAuthModeDerivation(t *testing.T) {
	t.Setenv("KEYCLOAK_ISSUER", "")
	t.Setenv("SDP_AUTH_MODE", "")
	t.Setenv("ARTIFACT_STORE_DRIVER", "")
	t.Setenv("HUB_AUDIT_LOG_RETENTION_DAYS", "")
	t.Setenv("HUB_RATE_LIMIT_RPS", "")
	t.Setenv("HUB_RATE_LIMIT_BURST", "")
	t.Setenv("HUB_CORS_ORIGINS", "")

	cfg := Load()
	if got := cfg.EffectiveAuthMode(); got != AuthModeDev {
		t.Fatalf("empty issuer should derive dev mode, got %q", got)
	}
	if !cfg.AuthDisabled() || cfg.OIDCEnabled() {
		t.Fatal("derived dev mode must keep the compatibility wrapper consistent")
	}
	if cfg.AuditLogRetentionDays != 0 || cfg.RetentionGCInterval != 0 || cfg.RateLimitRPS != 0 {
		t.Fatal("retention and rate-limit values must default to off")
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("default config must validate: %v", err)
	}
}

func TestLoadExplicitOIDCModeAndStrictParsing(t *testing.T) {
	t.Setenv("KEYCLOAK_ISSUER", "https://issuer.example/realms/sdp")
	t.Setenv("SDP_AUTH_MODE", "oidc")
	t.Setenv("HUB_AUDIT_LOG_RETENTION_DAYS", "not-an-int")
	t.Setenv("HUB_RATE_LIMIT_RPS", "4")
	t.Setenv("HUB_RATE_LIMIT_BURST", "")
	t.Setenv("HUB_CORS_ORIGINS", " https://a.example , https://b.example ")

	cfg := Load()
	if !cfg.OIDCEnabled() {
		t.Fatal("explicit oidc mode must enable OIDC")
	}
	if cfg.AuditLogRetentionDays != 0 {
		t.Fatal("malformed int must fall back to default")
	}
	if cfg.RateLimitBurst != 9 {
		t.Fatalf("burst default should be 2*rps+1=9, got %d", cfg.RateLimitBurst)
	}
	if len(cfg.CORSOrigins) != 2 || cfg.CORSOrigins[0] != "https://a.example" {
		t.Fatalf("CORS origins should be trimmed and split, got %v", cfg.CORSOrigins)
	}
	err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), "HUB_AUDIT_LOG_RETENTION_DAYS") {
		t.Fatalf("malformed int must fail validation, got %v", err)
	}
}

func TestValidateRejectsUnknownAuthMode(t *testing.T) {
	t.Setenv("KEYCLOAK_ISSUER", "")
	t.Setenv("SDP_AUTH_MODE", "sso")
	cfg := Load()
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "SDP_AUTH_MODE") {
		t.Fatalf("unknown mode must fail validation, got %v", err)
	}
}
