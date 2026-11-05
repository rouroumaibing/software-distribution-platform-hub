package keycloak

// Login-time grant + user-state operations (AUTH-ARCHITECTURE.md Flow A/B).
//
// These extend the narrow Admin-REST Client with the two OAuth grants the BFF
// auth handler needs (password grant for Flow A, authorization_code for Flow B)
// plus the user-state mutations the forced-change flow drives (look up
// requiredActions, clear them, logout-all sessions). The grant calls take an
// explicit clientID because the login client (sdp-console, public) differs from
// the service-account client (sdp-backend) the Client is configured as; the
// admin calls reuse the cached SA token (sdp-backend holds manage-users).

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// GrantResult is the subset of the OAuth token response the auth handler uses.
type GrantResult struct {
	AccessToken      string `json:"access_token"`
	IDToken          string `json:"id_token,omitempty"`
	ExpiresIn        int    `json:"expires_in"`
	RefreshExpiresIn int    `json:"refresh_expires_in,omitempty"`
	TokenType        string `json:"token_type,omitempty"`
	NotBeforePolicy  int    `json:"not-before-policy,omitempty"`
}

// Grant error sentinels. PasswordGrant returns these so the handler can map
// "Account is not fully set up" (credential verified + pending required action)
// distinctly from "Invalid user credentials" — the former is the forced-change
// trigger, the latter is a plain 401.
var (
	ErrAccountNotFullySetUp = errors.New("keycloak: account is not fully set up")
	ErrInvalidCredentials   = errors.New("keycloak: invalid user credentials")
)

// kcError is Keycloak's standard token-endpoint error body.
type kcError struct {
	Error            string `json:"error"`
	ErrorDescription string `json:"error_description"`
}

// PasswordGrant performs an OAuth password grant against the configured token
// endpoint using clientID (the public login client, no secret). Returns the
// grant on success; on 400 returns ErrAccountNotFullySetUp / ErrInvalidCredentials
// (classified by error_description) so the handler can branch the forced-change
// UX without string-matching in transport code.
func (c *Client) PasswordGrant(ctx context.Context, clientID, username, password string) (*GrantResult, error) {
	form := url.Values{}
	form.Set("grant_type", "password")
	form.Set("client_id", clientID)
	form.Set("username", username)
	form.Set("password", password)
	form.Set("scope", "openid profile email")
	return c.doGrant(ctx, form)
}

// ExchangeCode performs an OAuth authorization_code grant. codeVerifier is the
// PKCE verifier the caller held when generating the auth URL (AUTH-ARCHITECTURE
// §2 Flow B, D4).
func (c *Client) ExchangeCode(ctx context.Context, clientID, code, redirectURI, codeVerifier string) (*GrantResult, error) {
	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("client_id", clientID)
	form.Set("code", code)
	form.Set("redirect_uri", redirectURI)
	form.Set("code_verifier", codeVerifier)
	return c.doGrant(ctx, form)
}

func (c *Client) doGrant(ctx context.Context, form url.Values) (*GrantResult, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.TokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		var ke kcError
		_ = json.Unmarshal(body, &ke)
		switch {
		case strings.Contains(ke.ErrorDescription, "Account is not fully set up"):
			return nil, ErrAccountNotFullySetUp
		case strings.Contains(ke.ErrorDescription, "Invalid user credentials") || ke.Error == "invalid_grant" && strings.Contains(strings.ToLower(ke.ErrorDescription), "invalid"):
			return nil, ErrInvalidCredentials
		}
		return nil, fmt.Errorf("keycloak: token endpoint %d: %s", resp.StatusCode, string(body))
	}
	var g GrantResult
	if err := json.Unmarshal(body, &g); err != nil {
		return nil, err
	}
	if g.AccessToken == "" {
		return nil, fmt.Errorf("keycloak: token endpoint returned empty access_token")
	}
	return &g, nil
}

// LoginClientImpersonate performs an RFC 8693 token exchange: it trades a
// subject token (the login client's access token obtained via password grant or
// authorization_code) for a token minted for this hub's resource client
// (sdp-backend). The resource client validates the audience/azp of API bearer
// tokens, so sessions must carry it (AUTH-ARCHITECTURE §2.1 / §4, D2). The hub
// authenticates the exchange as sdp-backend itself (confidential, service
// account) using its client secret.
func (c *Client) LoginClientImpersonate(ctx context.Context, subjectToken string) (*GrantResult, error) {
	if strings.TrimSpace(subjectToken) == "" {
		return nil, fmt.Errorf("keycloak: subject token is required for token exchange")
	}
	form := url.Values{}
	form.Set("grant_type", "urn:ietf:params:oauth:grant-type:token-exchange")
	form.Set("client_id", c.ClientID)
	if c.ClientSecret != "" {
		form.Set("client_secret", c.ClientSecret)
	}
	form.Set("subject_token", subjectToken)
	form.Set("subject_token_type", "urn:ietf:params:oauth:token-type:access_token")
	return c.doGrant(ctx, form)
}

// GetUserByNameExact looks up a single user by exact username match. Returns
// ErrNotFound when no exact match. Used by the login handler to read
// requiredActions after a not-fully-set-up grant (confirm UPDATE_PASSWORD vs
// another pending action).
func (c *Client) GetUserByNameExact(ctx context.Context, username string) (User, error) {
	path := "/users?username=" + url.QueryEscape(username) + "&exact=true"
	var users []User
	status, _, err := c.adminCall(ctx, http.MethodGet, path, nil, &users)
	if err != nil {
		return User{}, err
	}
	if err := mapAdminStatus(status, nil); err != nil {
		return User{}, err
	}
	if len(users) == 0 {
		return User{}, ErrNotFound
	}
	return users[0], nil
}

// ClearRequiredActions removes all required actions from the user. Per
// AUTH-ARCHITECTURE §6 坑1: Go's omitempty drops an empty slice, so a PUT with
// requiredActions:[] would be silently omitted and KC would merge-keep the old
// actions. The portable workaround is [""] — a non-empty slice that serializes
// and that KC treats as "no actions". This is the one place the workaround is
// applied; everywhere else requiredActions is set explicitly.
func (c *Client) ClearRequiredActions(ctx context.Context, id string) error {
	rep := kcUserRep{RequiredActions: []string{""}}
	status, _, err := c.adminCall(ctx, http.MethodPut, "/users/"+url.PathEscape(id), rep, nil)
	if err != nil {
		return err
	}
	return mapAdminStatus(status, nil)
}

// LogoutAll revokes every session of the user (KC Admin REST
// POST /users/:id/logout — sets notBefore, invalidates issued tokens). Called
// after a successful password change so stale tokens cannot be reused.
func (c *Client) LogoutAll(ctx context.Context, id string) error {
	status, _, err := c.adminCall(ctx, http.MethodPost, "/users/"+url.PathEscape(id)+"/logout", nil, nil)
	if err != nil {
		return err
	}
	return mapAdminStatus(status, nil)
}

// IDTokenClaims is the subset of the OIDC ID token the session stores.
type IDTokenClaims struct {
	Subject           string   `json:"sub"`
	PreferredUsername string   `json:"preferred_username"`
	Email             string   `json:"email,omitempty"`
	Groups            []string `json:"groups,omitempty"`
}

// DecodeIDToken extracts the ID token payload without signature verification —
// the token was obtained directly from Keycloak's token endpoint over TLS by
// the hub, so transport integrity is guaranteed; signature re-verification would
// be redundant here (the JWT middleware path already covers API-bearer tokens).
func DecodeIDToken(idToken string) (IDTokenClaims, error) {
	parts := strings.Split(idToken, ".")
	if len(parts) < 2 {
		return IDTokenClaims{}, fmt.Errorf("keycloak: malformed id_token")
	}
	dec, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return IDTokenClaims{}, fmt.Errorf("keycloak: decode id_token payload: %w", err)
	}
	var c IDTokenClaims
	if err := json.Unmarshal(dec, &c); err != nil {
		return IDTokenClaims{}, fmt.Errorf("keycloak: unmarshal id_token claims: %w", err)
	}
	return c, nil
}
