package keycloak

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// DirectoryAdmin is the Admin REST surface behind the console's directory
// proxy (GET/POST /api/keycloak/users, /api/keycloak/groups — ui-05 §7.14 M1,
// ui-09 N-34). It reuses the same service-account authentication as group
// provisioning; the realm-management roles granted to that account bound the
// actual capability (seed: manage-users + query-groups). The proxy is
// management-plane only — it is mounted behind the platform-level user:manage
// guard and never participates in token verification.
type DirectoryAdmin interface {
	ListUsers(ctx context.Context, search string, first, max int) ([]User, error)
	CreateUser(ctx context.Context, in NewUser) (string, error)
	UpdateUser(ctx context.Context, id string, in UserPatch) error
	ResetPassword(ctx context.Context, id, password string, temporary bool) error
	ListGroups(ctx context.Context, search string) ([]Group, error)
	CreateGroup(ctx context.Context, name string) (string, error)
	GetGroup(ctx context.Context, id string) (Group, error)
	ListGroupMembers(ctx context.Context, groupID string, first, max int) ([]User, error)
	AddGroupMember(ctx context.Context, groupID, userID string) error
	RemoveGroupMember(ctx context.Context, groupID, userID string) error
}

// Sentinels mapped to HTTP statuses by the proxy handler. Plain errors (not
// *common.APIError) so this package stays free of transport concerns and the
// caller decides presentation.
var (
	ErrNotFound = errors.New("keycloak: entity not found")
	ErrConflict = errors.New("keycloak: entity already exists")
)

// User is the slim directory projection the console needs. It mirrors the
// Keycloak user representation fields the UI renders.
type User struct {
	ID              string   `json:"id"`
	Username        string   `json:"username"`
	Email           string   `json:"email,omitempty"`
	Enabled         bool     `json:"enabled"`
	RequiredActions []string `json:"requiredActions,omitempty"`
}

// Group is the directory projection of a Keycloak group; Path carries the
// full path (e.g. "/org:team-a") the /org: read-only guard matches on.
type Group struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Path string `json:"path"`
}

// NewUser is the create-user input. InitialPassword, when set, is stored as a
// temporary credential so Keycloak forces a change on first login.
type NewUser struct {
	Username        string
	Email           string
	Enabled         bool
	RequiredActions []string
	InitialPassword string
}

// UserPatch is the update-user input; nil fields are left untouched.
type UserPatch struct {
	Enabled *bool
	Email   *string
}

// kcUserRep is the Keycloak-side user representation for create/update.
type kcUserRep struct {
	Username        string         `json:"username,omitempty"`
	Email           string         `json:"email,omitempty"`
	Enabled         bool           `json:"enabled"`
	RequiredActions []string       `json:"requiredActions,omitempty"`
	Credentials     []kcCredential `json:"credentials,omitempty"`
}

type kcCredential struct {
	Type      string `json:"type"`
	Value     string `json:"value"`
	Temporary bool   `json:"temporary"`
}

// adminCall performs one authenticated Admin REST call. in (optional) is sent
// as the JSON body; out (optional) receives the decoded JSON response. It
// returns the response status and headers so callers can interpret
// create-with-Location semantics and map statuses to sentinels.
func (c *Client) adminCall(ctx context.Context, method, path string, in, out any) (int, http.Header, error) {
	tok, err := c.token(ctx)
	if err != nil {
		return 0, nil, err
	}
	var body io.Reader
	if in != nil {
		buf, merr := json.Marshal(in)
		if merr != nil {
			return 0, nil, merr
		}
		body = bytes.NewReader(buf)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.AdminBaseURL+path, body)
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if out != nil && resp.StatusCode == http.StatusOK && len(raw) > 0 {
		if derr := json.Unmarshal(raw, out); derr != nil {
			return resp.StatusCode, resp.Header, derr
		}
	}
	return resp.StatusCode, resp.Header, nil
}

func mapAdminStatus(status int, raw []byte) error {
	switch status {
	case http.StatusOK, http.StatusCreated, http.StatusNoContent:
		return nil
	case http.StatusNotFound:
		return ErrNotFound
	case http.StatusConflict:
		return ErrConflict
	}
	return fmt.Errorf("keycloak: admin call %d: %s", status, string(raw))
}

func (c *Client) ListUsers(ctx context.Context, search string, first, max int) ([]User, error) {
	q := url.Values{}
	if search != "" {
		q.Set("search", search)
	}
	if first > 0 {
		q.Set("first", strconv.Itoa(first))
	}
	if max > 0 {
		q.Set("max", strconv.Itoa(max))
	}
	path := "/users"
	if len(q) > 0 {
		path += "?" + q.Encode()
	}
	var users []User
	status, _, err := c.adminCall(ctx, http.MethodGet, path, nil, &users)
	if err != nil {
		return nil, err
	}
	if err := mapAdminStatus(status, nil); err != nil {
		return nil, err
	}
	return users, nil
}

func (c *Client) CreateUser(ctx context.Context, in NewUser) (string, error) {
	if strings.TrimSpace(in.Username) == "" {
		return "", fmt.Errorf("keycloak: username is required")
	}
	rep := kcUserRep{Username: in.Username, Email: in.Email, Enabled: in.Enabled, RequiredActions: in.RequiredActions}
	if in.InitialPassword != "" {
		rep.Credentials = []kcCredential{{Type: "password", Value: in.InitialPassword, Temporary: true}}
	}
	status, hdr, err := c.adminCall(ctx, http.MethodPost, "/users", rep, nil)
	if err != nil {
		return "", err
	}
	if serr := mapAdminStatus(status, nil); serr != nil {
		return "", serr
	}
	// 201 carries the new user in the Location header: .../users/<id>.
	if loc := hdr.Get("Location"); loc != "" {
		if i := strings.LastIndex(loc, "/users/"); i >= 0 {
			return strings.TrimPrefix(loc[i:], "/users/"), nil
		}
	}
	return "", nil
}

func (c *Client) UpdateUser(ctx context.Context, id string, in UserPatch) error {
	if id == "" {
		return fmt.Errorf("keycloak: user id is required")
	}
	rep := kcUserRep{}
	if in.Enabled != nil {
		rep.Enabled = *in.Enabled
	}
	if in.Email != nil {
		rep.Email = *in.Email
	}
	status, _, err := c.adminCall(ctx, http.MethodPut, "/users/"+url.PathEscape(id), rep, nil)
	if err != nil {
		return err
	}
	return mapAdminStatus(status, nil)
}

func (c *Client) ResetPassword(ctx context.Context, id, password string, temporary bool) error {
	if id == "" || password == "" {
		return fmt.Errorf("keycloak: user id and password are required")
	}
	cred := kcCredential{Type: "password", Value: password, Temporary: temporary}
	status, _, err := c.adminCall(ctx, http.MethodPut, "/users/"+url.PathEscape(id)+"/reset-password", cred, nil)
	if err != nil {
		return err
	}
	return mapAdminStatus(status, nil)
}

func (c *Client) ListGroups(ctx context.Context, search string) ([]Group, error) {
	path := "/groups"
	if search != "" {
		path += "?search=" + url.QueryEscape(search)
	}
	var groups []Group
	status, _, err := c.adminCall(ctx, http.MethodGet, path, nil, &groups)
	if err != nil {
		return nil, err
	}
	if err := mapAdminStatus(status, nil); err != nil {
		return nil, err
	}
	return groups, nil
}

// CreateGroup creates a top-level group and returns its id (empty when the
// server does not surface a Location). Conflicts surface as ErrConflict —
// unlike EnsureGroup, which treats them as success.
func (c *Client) CreateGroup(ctx context.Context, name string) (string, error) {
	id, conflict, err := c.doCreateGroup(ctx, name)
	if err != nil {
		return "", err
	}
	if conflict {
		return "", ErrConflict
	}
	return id, nil
}

func (c *Client) GetGroup(ctx context.Context, id string) (Group, error) {
	if id == "" {
		return Group{}, fmt.Errorf("keycloak: group id is required")
	}
	var g Group
	status, _, err := c.adminCall(ctx, http.MethodGet, "/groups/"+url.PathEscape(id), nil, &g)
	if err != nil {
		return Group{}, err
	}
	if serr := mapAdminStatus(status, nil); serr != nil {
		return Group{}, serr
	}
	return g, nil
}

func (c *Client) ListGroupMembers(ctx context.Context, groupID string, first, max int) ([]User, error) {
	if groupID == "" {
		return nil, fmt.Errorf("keycloak: group id is required")
	}
	q := url.Values{}
	if first > 0 {
		q.Set("first", strconv.Itoa(first))
	}
	if max > 0 {
		q.Set("max", strconv.Itoa(max))
	}
	path := "/groups/" + url.PathEscape(groupID) + "/members"
	if len(q) > 0 {
		path += "?" + q.Encode()
	}
	var users []User
	status, _, err := c.adminCall(ctx, http.MethodGet, path, nil, &users)
	if err != nil {
		return nil, err
	}
	if serr := mapAdminStatus(status, nil); serr != nil {
		return nil, serr
	}
	return users, nil
}

func (c *Client) AddGroupMember(ctx context.Context, groupID, userID string) error {
	if groupID == "" || userID == "" {
		return fmt.Errorf("keycloak: group id and user id are required")
	}
	status, _, err := c.adminCall(ctx, http.MethodPut, "/groups/"+url.PathEscape(groupID)+"/members/"+url.PathEscape(userID), nil, nil)
	if err != nil {
		return err
	}
	return mapAdminStatus(status, nil)
}

func (c *Client) RemoveGroupMember(ctx context.Context, groupID, userID string) error {
	if groupID == "" || userID == "" {
		return fmt.Errorf("keycloak: group id and user id are required")
	}
	status, _, err := c.adminCall(ctx, http.MethodDelete, "/groups/"+url.PathEscape(groupID)+"/members/"+url.PathEscape(userID), nil, nil)
	if err != nil {
		return err
	}
	return mapAdminStatus(status, nil)
}
