package middleware

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/gin-gonic/gin"

	"github.com/rouroumaibing/software-distribution-platform-hub/internal/common"
)

// contextKeyClaims is where AuthMiddleware stashes the verified token
// claims for downstream middleware/handlers to read via c.Get.
const contextKeyClaims = "auth.claims"

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
}

// Authenticator verifies bearer tokens against a single Keycloak realm.
type Authenticator struct {
	verifier *oidc.IDTokenVerifier
	clientID string
}

// NewAuthenticator fetches the realm's OIDC discovery document once at
// startup. Fails fast if Keycloak is unreachable or the realm doesn't
// exist — better to crash on boot than accept unverifiable tokens.
func NewAuthenticator(ctx context.Context, cfg AuthConfig) (*Authenticator, error) {
	provider, err := oidc.NewProvider(ctx, cfg.IssuerURL)
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

// Middleware verifies the Authorization: Bearer <token> header against
// Keycloak's public keys (signature, expiry, issuer) and stores the
// decoded claims in the gin context. It does not touch the database —
// UserContext (user_context.go) handles turning claims into a local User.
func (a *Authenticator) Middleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		header := c.GetHeader("Authorization")
		rawToken, ok := strings.CutPrefix(header, "Bearer ")
		if !ok || rawToken == "" {
			common.Fail(c, http.StatusUnauthorized, errMissingBearer)
			c.Abort()
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
