// Package handler — auth endpoints (AUTH-ARCHITECTURE.md §7).
//
// Five pre-auth endpoints mounted on /api/v1/auth/* (NO auth middleware):
//   GET  /login-url     — Flow B entry: KC auth URL + PKCE (backend holds verifier)
//   POST /login         — Flow A entry: password grant; 400 reason=UPDATE_PASSWORD
//                         surfaces forced change; 401 invalid credentials
//   POST /session/code  — Flow B callback: code exchange → session
//   POST /password      — forced change: verify old (allow not-fully-set-up) →
//                         ResetPassword temporary=false → clear RA → logout-all
//   POST /logout        — revoke session + clear cookie
//
// On a successful grant (Flow A login / Flow B code) establishSession decodes
// the ID token, writes a session row, and sets the httpOnly cookie. The browser
// never sees the Keycloak token (D5).
package handler

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/rouroumaibing/software-distribution-platform-hub/internal/common"
	"github.com/rouroumaibing/software-distribution-platform-hub/internal/keycloak"
	"github.com/rouroumaibing/software-distribution-platform-hub/internal/session"
)

const (
	// verifierTTL — a Flow B login must complete within this window of
	// getLoginUrl. Short to bound the in-memory store.
	verifierTTL = 10 * time.Minute
	// minNewPassword — enforced on the forced-change path. Mirrors the
	// directory reset-password floor only loosely (self-service is lighter).
	minNewPassword = 8
)

type AuthHandler struct {
	kc            *keycloak.Client
	sessions      *session.Repository
	issuer        string // KC realm URL, e.g. https://host/keycloak/realms/sdp
	loginClientID string // public login client (sdp-console)
	redirectURI   string // console OAuth callback URL (Flow B)
	secureCookie  bool

	verifiers *verifierStore
}

func NewAuthHandler(kc *keycloak.Client, sessions *session.Repository, issuer, loginClientID, redirectURI string, secureCookie bool) *AuthHandler {
	return &AuthHandler{
		kc:            kc,
		sessions:      sessions,
		issuer:        strings.TrimRight(issuer, "/"),
		loginClientID: loginClientID,
		redirectURI:   redirectURI,
		secureCookie:  secureCookie,
		verifiers:     newVerifierStore(),
	}
}

// RegisterRoutes mounts the five endpoints on a router group that has NO auth
// middleware (these are pre-auth). Caller passes a bare group, e.g.
// `r.Group("/api/v1/auth")`.
func (h *AuthHandler) RegisterRoutes(rg *gin.RouterGroup) {
	rg.GET("/login-url", h.LoginURL)
	rg.POST("/login", h.Login)
	rg.POST("/session/code", h.SessionCode)
	rg.POST("/password", h.Password)
	rg.POST("/logout", h.Logout)
}

// --- Flow B entry ---

type loginURLResp struct {
	LoginURL string `json:"loginUrl"`
	State    string `json:"state"`
}

func (h *AuthHandler) LoginURL(c *gin.Context) {
	state := randomHex(16)
	verifier := generateCodeVerifier()
	challenge := s256Challenge(verifier)
	h.verifiers.set(state, verifier)

	q := url.Values{}
	q.Set("client_id", h.loginClientID)
	q.Set("response_type", "code")
	q.Set("scope", "openid profile email")
	q.Set("redirect_uri", h.redirectURI)
	q.Set("state", state)
	q.Set("code_challenge", challenge)
	q.Set("code_challenge_method", "S256")
	loginURL := h.issuer + "/protocol/openid-connect/auth?" + q.Encode()
	common.OKNoEscape(c, loginURLResp{LoginURL: loginURL, State: state})
}

func (h *AuthHandler) SessionCode(c *gin.Context) {
	var req struct {
		Code  string `json:"code"`
		State string `json:"state"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		common.Fail(c, http.StatusBadRequest, err)
		return
	}
	verifier, ok := h.verifiers.take(req.State)
	if !ok {
		common.Fail(c, http.StatusBadRequest,
			common.DomainError(common.KindAuth, http.StatusBadRequest, 42, "invalid or expired login state"))
		return
	}
	g, err := h.kc.ExchangeCode(c.Request.Context(), h.loginClientID, req.Code, h.redirectURI, verifier)
	if err != nil {
		common.Fail(c, http.StatusBadRequest, err)
		return
	}
	if err := h.establishSession(c, g); err != nil {
		common.Fail(c, http.StatusInternalServerError, err)
		return
	}
	common.OK(c, gin.H{"authenticated": true})
}

// --- Flow A entry ---

type loginReq struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

func (h *AuthHandler) Login(c *gin.Context) {
	var req loginReq
	if err := c.ShouldBindJSON(&req); err != nil {
		common.Fail(c, http.StatusBadRequest, err)
		return
	}
	g, err := h.kc.PasswordGrant(c.Request.Context(), h.loginClientID, req.Username, req.Password)
	if err != nil {
		switch {
		case errors.Is(err, keycloak.ErrAccountNotFullySetUp):
			// Confirm which required action is pending (UPDATE_PASSWORD vs other).
			reason := "UPDATE_PASSWORD"
			if u, gerr := h.kc.GetUserByNameExact(c.Request.Context(), req.Username); gerr == nil && len(u.RequiredActions) > 0 {
				reason = u.RequiredActions[0]
			}
			common.Fail(c, http.StatusBadRequest,
				common.DomainErrorWithReasons(common.KindAuth, http.StatusBadRequest, 40,
					"account is not fully set up", reason))
			return
		case errors.Is(err, keycloak.ErrInvalidCredentials):
			common.Fail(c, http.StatusUnauthorized,
				common.DomainError(common.KindAuth, http.StatusUnauthorized, 41, "invalid username or password"))
			return
		default:
			common.Fail(c, http.StatusInternalServerError, err)
			return
		}
	}
	if err := h.establishSession(c, g); err != nil {
		common.Fail(c, http.StatusInternalServerError, err)
		return
	}
	common.OK(c, gin.H{"authenticated": true})
}

// --- forced change ---

type passwordReq struct {
	Username    string `json:"username"`
	OldPassword string `json:"oldPassword"`
	NewPassword string `json:"newPassword"`
}

func (h *AuthHandler) Password(c *gin.Context) {
	var req passwordReq
	if err := c.ShouldBindJSON(&req); err != nil {
		common.Fail(c, http.StatusBadRequest, err)
		return
	}
	if len(req.NewPassword) < minNewPassword {
		common.Fail(c, http.StatusBadRequest,
			common.DomainError(common.KindAuth, http.StatusBadRequest, 43, fmt.Sprintf("new password must be at least %d characters", minNewPassword)))
		return
	}
	if req.NewPassword == req.OldPassword {
		common.Fail(c, http.StatusBadRequest,
			common.DomainError(common.KindAuth, http.StatusBadRequest, 44, "new password must differ from the old password"))
		return
	}

	// 1. Verify old password via password grant. Per AUTH-ARCHITECTURE §6 坑2,
	// "Account is not fully set up" is returned AFTER credential verification
	// succeeds — so it counts as old-password-correct and we proceed. Any other
	// error is a real rejection.
	_, err := h.kc.PasswordGrant(c.Request.Context(), h.loginClientID, req.Username, req.OldPassword)
	if err != nil && !errors.Is(err, keycloak.ErrAccountNotFullySetUp) {
		if errors.Is(err, keycloak.ErrInvalidCredentials) {
			common.Fail(c, http.StatusUnauthorized,
				common.DomainError(common.KindAuth, http.StatusUnauthorized, 41, "invalid old password"))
			return
		}
		common.Fail(c, http.StatusInternalServerError, err)
		return
	}

	// 2. Resolve the user id (for ResetPassword / ClearRequiredActions / LogoutAll).
	u, err := h.kc.GetUserByNameExact(c.Request.Context(), req.Username)
	if err != nil {
		common.Fail(c, http.StatusInternalServerError, err)
		return
	}

	// 3. Set the new password as a permanent credential (temporary=false, 坑3).
	if err := h.kc.ResetPassword(c.Request.Context(), u.ID, req.NewPassword, false); err != nil {
		common.Fail(c, http.StatusInternalServerError, err)
		return
	}
	// 4. Clear required actions. Per 坑1, [""] is the portable workaround for
	// Go's omitempty dropping an empty slice.
	if err := h.kc.ClearRequiredActions(c.Request.Context(), u.ID); err != nil {
		common.Fail(c, http.StatusInternalServerError, err)
		return
	}
	// 5. Revoke stale KC sessions (best-effort — a failure here doesn't undo
	// the password change).
	_ = h.kc.LogoutAll(c.Request.Context(), u.ID)

	// No session established here: per AUTH-ARCHITECTURE §3, the frontend
	// switches back to login mode and authenticates with the new password,
	// which establishes the session via /login. Returning the user id lets the
	// frontend confirm the target without a second lookup.
	common.OK(c, gin.H{"changed": true, "subject": u.ID})
}

// --- logout ---

func (h *AuthHandler) Logout(c *gin.Context) {
	if sid := session.ReadCookie(c.Request); sid != "" {
		_ = h.sessions.Delete(c.Request.Context(), sid)
	}
	session.ClearCookie(c.Writer)
	c.Status(http.StatusNoContent)
}

// --- shared: grant → session row + cookie ---

func (h *AuthHandler) establishSession(c *gin.Context, g *keycloak.GrantResult) error {
	if g.IDToken == "" {
		return errors.New("auth: grant missing id_token (scope must include openid)")
	}
	claims, err := keycloak.DecodeIDToken(g.IDToken)
	if err != nil {
		return fmt.Errorf("auth: decode id token: %w", err)
	}
	groupsJSON, _ := json.Marshal(claims.Groups)
	s := &session.Session{
		Subject:   claims.Subject,
		Username:  claims.PreferredUsername,
		Groups:    string(groupsJSON),
		KCToken:   g.AccessToken,
		ExpiresAt: time.Now().Add(session.CookieMaxAge),
	}
	if err := h.sessions.Create(c.Request.Context(), s); err != nil {
		return fmt.Errorf("auth: create session: %w", err)
	}
	return session.SetCookie(c.Writer, s.ID, h.secureCookie)
}

// --- PKCE + verifier store ---

func generateCodeVerifier() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		// crypto/rand failure invalidates the whole OAuth flow — same stance as
		// the directory temp-password generator (panic rather than degrade).
		panic("auth: crypto/rand failed: " + err.Error())
	}
	return base64.RawURLEncoding.EncodeToString(b) // 43 chars, RFC 7636 compliant
}

func s256Challenge(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func randomHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic("auth: crypto/rand failed: " + err.Error())
	}
	return fmt.Sprintf("%x", b)
}

type verifierStore struct {
	mu sync.Mutex
	m  map[string]verifierEntry
}

type verifierEntry struct {
	verifier string
	exp      time.Time
}

func newVerifierStore() *verifierStore {
	s := &verifierStore{m: make(map[string]verifierEntry)}
	go s.gc()
	return s
}

func (s *verifierStore) set(state, verifier string) {
	s.mu.Lock()
	s.m[state] = verifierEntry{verifier: verifier, exp: time.Now().Add(verifierTTL)}
	s.mu.Unlock()
}

// take removes and returns the verifier for state (one-shot — a state is
// single-use). Returns false if missing or expired.
func (s *verifierStore) take(state string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.m[state]
	if !ok || time.Now().After(e.exp) {
		delete(s.m, state)
		return "", false
	}
	delete(s.m, state)
	return e.verifier, true
}

func (s *verifierStore) gc() {
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	for range t.C {
		now := time.Now()
		s.mu.Lock()
		for k, e := range s.m {
			if now.After(e.exp) {
				delete(s.m, k)
			}
		}
		s.mu.Unlock()
	}
}
