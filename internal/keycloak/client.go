// Package keycloak contains a deliberately small Keycloak Admin REST client.
//
// It exists for exactly one reason: the hub creates Orgs at runtime (via the
// API), and each Org needs a carrier group `/org:<slug>` in Keycloak so the
// org dimension shows up in the token's `groups` claim (ACCOUNT-PERMISSION-
// MODEL §2.3). Keycloak Organizations import is not relied upon, and a
// pre-provisioning seed job cannot know future org slugs — so the hub, acting
// as the existing `sdp-backend` service account, creates the group on demand.
//
// Design constraints (steelman, 2026-09-23):
//   - No Keycloak Go SDK dependency: the client is fully unit-testable behind
//     httptest, which keeps `go test ./...` hermetic.
//   - Narrow scope: only mint a service-account token + create/list groups.
//     The hub stays a *passive Resource Server* for token verification; this
//     client is a *controlled writer* of one resource type, not a general admin.
//   - Best-effort by contract: callers decide whether a failure is fatal. The
//     org DB row is the source of truth; the group is a convenience carrier.
package keycloak

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// GroupProvisioner is the narrow capability the org service needs: make sure a
// group exists at the given full path (e.g. "/org:team-a"). It is an interface
// so the org service can be unit-tested without a live Keycloak.
type GroupProvisioner interface {
	// EnsureGroup is idempotent: if a group with the exact path already exists
	// the call is a no-op and returns nil.
	EnsureGroup(ctx context.Context, groupPath string) error
}

// Client performs the narrow set of Admin REST calls described above.
type Client struct {
	AdminBaseURL string // e.g. https://host/keycloak/admin/realms/sdp
	TokenURL     string // e.g. https://host/keycloak/realms/sdp/protocol/openid-connect/token
	ClientID     string
	ClientSecret string
	HTTP         *http.Client

	mu          sync.Mutex
	cachedToken string
	cachedExp   time.Time
}

// NewClient builds a Client from an OIDC issuer URL (e.g.
// "https://host/keycloak/realms/sdp"). The admin REST base is derived by
// rewriting "/realms/" to "/admin/realms/" on the same host, because the Admin
// API lives under /admin while the token endpoint lives under /realms.
func NewClient(issuer, clientID, clientSecret string) *Client {
	adminBase := strings.Replace(issuer, "/realms/", "/admin/realms/", 1)
	if adminBase == issuer {
		// issuer did not contain /realms/ — fall back to a fixed suffix so the
		// client is still usable instead of silently producing a bad URL.
		adminBase = strings.TrimRight(issuer, "/") + "/admin/realms/sdp"
	}
	return &Client{
		AdminBaseURL: adminBase,
		TokenURL:     strings.TrimRight(issuer, "/") + "/protocol/openid-connect/token",
		ClientID:     clientID,
		ClientSecret: clientSecret,
		HTTP:         &http.Client{Timeout: 15 * time.Second},
	}
}

// EnsureGroup makes sure a group exists at groupPath (full path, e.g.
// "/org:team-a"). It is idempotent.
func (c *Client) EnsureGroup(ctx context.Context, groupPath string) error {
	name := strings.TrimPrefix(groupPath, "/")
	if name == "" {
		return fmt.Errorf("keycloak: empty group path")
	}
	exists, err := c.groupExists(ctx, name)
	if err != nil {
		return err
	}
	if exists {
		return nil
	}
	return c.createGroup(ctx, name)
}

// token returns a cached service-account token or mints a new one. The mutex
// guards the cache; the 30s skew keeps us from using a token that expires
// mid-request.
func (c *Client) token(ctx context.Context) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.cachedToken != "" && time.Now().Before(c.cachedExp) {
		return c.cachedToken, nil
	}
	form := url.Values{}
	form.Set("grant_type", "client_credentials")
	form.Set("client_id", c.ClientID)
	form.Set("client_secret", c.ClientSecret)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.TokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("keycloak: token endpoint %d: %s", resp.StatusCode, string(body))
	}
	var tr tokenResp
	if err := json.NewDecoder(resp.Body).Decode(&tr); err != nil {
		return "", err
	}
	if tr.AccessToken == "" {
		return "", fmt.Errorf("keycloak: token endpoint returned empty access_token")
	}
	c.cachedToken = tr.AccessToken
	c.cachedExp = time.Now().Add(time.Duration(tr.ExpiresIn)*time.Second - 30*time.Second)
	return tr.AccessToken, nil
}

// groupExists reports whether a group with the exact path "/<name>" exists.
func (c *Client) groupExists(ctx context.Context, name string) (bool, error) {
	tok, err := c.token(ctx)
	if err != nil {
		return false, err
	}
	u := c.AdminBaseURL + "/groups?search=" + url.QueryEscape(name) + "&exact=true"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return false, err
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return false, fmt.Errorf("keycloak: list groups %d: %s", resp.StatusCode, string(body))
	}
	var groups []struct {
		Name string `json:"name"`
		Path string `json:"path"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&groups); err != nil {
		return false, err
	}
	for _, g := range groups {
		if g.Path == "/"+name {
			return true, nil
		}
	}
	return false, nil
}

// createGroup POSTs a new group. 201 Created is success; 409 Conflict means a
// concurrent request already created it, which we treat as success (idempotent).
func (c *Client) createGroup(ctx context.Context, name string) error {
	tok, err := c.token(ctx)
	if err != nil {
		return err
	}
	body, err := json.Marshal(map[string]string{"name": name, "path": "/" + name})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.AdminBaseURL+"/groups", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusCreated || resp.StatusCode == http.StatusConflict {
		return nil
	}
	b, _ := io.ReadAll(resp.Body)
	return fmt.Errorf("keycloak: create group %q %d: %s", name, resp.StatusCode, string(b))
}

type tokenResp struct {
	AccessToken string `json:"access_token"`
	ExpiresIn   int    `json:"expires_in"`
}
