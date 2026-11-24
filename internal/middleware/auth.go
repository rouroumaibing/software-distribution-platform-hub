package middleware

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/gin-gonic/gin"

	"github.com/rouroumaibing/software-distribution-platform-hub/internal/common"
	"github.com/rouroumaibing/software-distribution-platform-hub/internal/common/logger"
)

// contextKeyClaims is where AuthMiddleware stashes the verified token
// claims for downstream middleware/handlers to read via c.Get.
const contextKeyClaims = "auth.claims"

// patTokenPrefix routes bearer tokens into the PAT verification path (§6.4).
const patTokenPrefix = "sdp_pat_"

var (
	errMissingBearer = errors.New("missing bearer token")
	errWrongClient   = errors.New("token was not issued for this client")
)

// keycloakClaims is the subset of the access token's claims the hub reads.
// Deliberately does NOT read realm_access/resource_access roles — those
// live in Keycloak's own role model, which this platform doesn't use for
// authorization (see internal/permission for why: component-scoped RBAC
// doesn't map cleanly onto realm/client-wide roles).
type keycloakClaims struct {
	Subject           string `json:"sub"`
	PreferredUsername string `json:"preferred_username"`
	Email             string `json:"email"`
	EmailVerified     bool   `json:"email_verified"`
	// Groups carries the Keycloak group memberships (configured via a groups
	// claim mapper). Consumed by §7 group-based RBAC inheritance so a single
	// group binding grants every member the role's actions.
	Groups []string `json:"groups"`
	// OrgAdmins carries the L2 organization-admin dimension (ACCESS-CONTROL
	// §1.2): the org names the subject administers, sourced from the user's
	// Keycloak `org_admin` attribute (mapped to the top-level claim by the
	// sdp-console client). Frontend-only visibility input, never an
	// authorization input (不动式①).
	OrgAdmins []string `json:"org_admin"`
	// AuthorizedParty is the client ID the token was issued for. Used
	// instead of the standard "aud" check below.
	AuthorizedParty string `json:"azp"`
}

// AuthConfig holds the Keycloak realm connection details.
type AuthConfig struct {
	// IssuerURL, e.g. "https://auth.example.com/realms/sdp". go-oidc
	// fetches /.well-known/openid-configuration from this to discover the
	// JWKS endpoint and auto-refresh signing keys.
	IssuerURL string
	// ClientID is this app's Keycloak client ID (the console's public
	// client, e.g. "sdp-console"), checked against the token's azp claim.
	ClientID string
	// DiscoveryAttempts / DiscoveryInterval bound the discovery retry. Zero
	// values use defaults (see defaultDiscoveryAttempts). Keycloak warm-up
	// returns 503/connection-refused while it boots; discovery must ride out
	// that window rather than treat it as a permanent config error.
	DiscoveryAttempts int
	DiscoveryInterval time.Duration
}

const (
	// The budget covers the dependency's readiness ceiling: Keycloak's
	// startupProbe allows failureThreshold=60 × periodSeconds=5s = 300s.
	// Finishing discovery before that ceiling would let hub give up while
	// Keycloak is still legitimately warming up.
	defaultDiscoveryAttempts = 150
	defaultDiscoveryInterval = 2 * time.Second
)

// Authenticator verifies bearer tokens against a single Keycloak realm.
type Authenticator struct {
	verifier *oidc.IDTokenVerifier
	clientID string
	// patVerifier resolves `sdp_pat_*` bearer tokens (§6.4) to their owner's
	// subject. Nil = PAT 鉴权未装配（纯 JWT）。
	patVerifier func(rawToken string) (subject string, err error)
	// sessionVerifier resolves the httpOnly session cookie (AUTH-ARCHITECTURE
	// §8) to an identity. Highest-priority path: when present and the cookie
	// is valid, the bearer/PAT paths are skipped entirely. Nil = BFF session
	// layer not wired (pure JWT/PAT, the pre-BFF behavior).
	sessionVerifier func(*http.Request) (SessionIdentity, error)
}

// SessionIdentity is the resolved identity from a session cookie, returned by
// the session verifier and mapped onto keycloakClaims inside the middleware
// (kept unexported; callers outside the package never construct it directly).
type SessionIdentity struct {
	Subject   string
	Username  string
	Groups    []string
	OrgAdmins []string
}

// SetPATVerifier wires §6.4 PAT verification. The verifier MUST fail-closed
// on unknown / revoked / expired tokens.
func (a *Authenticator) SetPATVerifier(fn func(rawToken string) (subject string, err error)) {
	a.patVerifier = fn
}

// SetSessionVerifier wires the BFF session-cookie path (AUTH-ARCHITECTURE §8).
// The verifier reads the cookie from the request, looks up the session row,
// and returns the identity — or an error when no valid session exists, in
// which case the middleware falls through to the bearer path. A missing or
// expired cookie MUST be an error, not a panic: the fall-through is designed.
func (a *Authenticator) SetSessionVerifier(fn func(*http.Request) (SessionIdentity, error)) {
	a.sessionVerifier = fn
}

// NewAuthenticator fetches the realm's OIDC discovery document at startup.
// Transient failures (Keycloak still booting: 503 / connection refused /
// realm not imported yet) are retried within a bounded window; permanent
// misconfiguration (issuer mismatch) fails immediately. It never accepts
// unverifiable tokens — it only waits for a dependency that is converging.
func NewAuthenticator(ctx context.Context, cfg AuthConfig) (*Authenticator, error) {
	provider, err := discoverProvider(ctx, cfg)
	if err != nil {
		return nil, err
	}

	// SkipClientIDCheck: Keycloak's default access-token audience is
	// "account", not this client's ID — a dedicated "audience" mapper
	// would be needed on the client scope to change that. Checking azp
	// (authorized party, which Keycloak always sets correctly) below is
	// simpler than requiring every realm to be configured with a custom
	// audience mapper.
	verifier := provider.Verifier(&oidc.Config{SkipClientIDCheck: true})
	return &Authenticator{verifier: verifier, clientID: cfg.ClientID}, nil
}

// discoverProvider performs a bounded retry around the OIDC discovery fetch.
// go-oidc exposes no structured status on discovery errors, so the split is:
// IssuerMismatchError is a deterministic misconfiguration (abort now); every
// other error is treated as a converging dependency (503, refused, realm not
// yet imported) and retried until the budget is exhausted. A wrong-but-
// reachable issuer therefore costs the bounded window before failing, which
// is the safe trade: transient warm-up must never take the hub down.
func discoverProvider(ctx context.Context, cfg AuthConfig) (*oidc.Provider, error) {
	attempts := cfg.DiscoveryAttempts
	if attempts <= 0 {
		attempts = defaultDiscoveryAttempts
	}
	interval := cfg.DiscoveryInterval
	if interval <= 0 {
		interval = defaultDiscoveryInterval
	}

	var lastErr error
	for i := 1; i <= attempts; i++ {
		provider, err := oidc.NewProvider(ctx, cfg.IssuerURL)
		if err == nil {
			return provider, nil
		}
		lastErr = err
		var mismatch *oidc.IssuerMismatchError
		if errors.As(err, &mismatch) {
			return nil, err
		}
		if i == attempts {
			break
		}
		logger.Warnf("hub: OIDC discovery attempt %d/%d failed (%v); retrying in %s", i, attempts, err, interval)
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(interval):
		}
	}
	return nil, lastErr
}

// Middleware verifies the Authorization: Bearer <token> header against
// Keycloak's public keys (signature, expiry, issuer) and stores the
// decoded claims in the gin context. It does not touch the database —
// UserContext (user_context.go) handles turning claims into a local User.
func (a *Authenticator) Middleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		// Session cookie path (AUTH-ARCHITECTURE §8, highest priority). When
		// the BFF session layer is wired and the request carries a valid
		// httpOnly session cookie, resolve claims from it and short-circuit
		// the bearer/PAT paths. A missing/expired cookie falls through.
		if a.sessionVerifier != nil {
			if id, verr := a.sessionVerifier(c.Request); verr == nil {
				c.Set(contextKeyClaims, keycloakClaims{
					Subject:           id.Subject,
					PreferredUsername: id.Username,
					Groups:            id.Groups,
					OrgAdmins:         id.OrgAdmins,
					AuthorizedParty:   a.clientID,
				})
				c.Next()
				return
			}
		}
		header := c.GetHeader("Authorization")
		rawToken, ok := strings.CutPrefix(header, "Bearer ")
		if !ok || rawToken == "" {
			common.Fail(c, http.StatusUnauthorized, errMissingBearer)
			c.Abort()
			return
		}

		// §6.4：PAT 走独立校验链（哈希查表，不经 Keycloak）。以属主身份注入
		// claims —— RBAC 按 subject 绑定照常生效，Groups 为空。
		if strings.HasPrefix(rawToken, patTokenPrefix) && a.patVerifier != nil {
			subject, verr := a.patVerifier(rawToken)
			if verr != nil {
				common.Fail(c, http.StatusUnauthorized, verr)
				c.Abort()
				return
			}
			c.Set(contextKeyClaims, keycloakClaims{Subject: subject, AuthorizedParty: a.clientID})
			c.Next()
			return
		}

		idToken, err := a.verifier.Verify(c.Request.Context(), rawToken)
		if err != nil {
			common.Fail(c, http.StatusUnauthorized, err)
			c.Abort()
			return
		}

		var claims keycloakClaims
		if err := idToken.Claims(&claims); err != nil {
			common.Fail(c, http.StatusUnauthorized, err)
			c.Abort()
			return
		}
		if claims.AuthorizedParty != a.clientID {
			common.Fail(c, http.StatusUnauthorized, errWrongClient)
			c.Abort()
			return
		}

		c.Set(contextKeyClaims, claims)
		c.Next()
	}
}
