package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/rouroumaibing/software-distribution-platform-hub/internal/common"
	"github.com/rouroumaibing/software-distribution-platform-hub/internal/keycloak"
)

// stubDirectory is a minimal in-memory KeycloakDirectory for handler tests.
type stubDirectory struct {
	users    []keycloak.User
	groups   map[string]keycloak.Group
	members  map[string][]keycloak.User
	createID string
}

func newStubDirectory() *stubDirectory {
	return &stubDirectory{
		groups:  map[string]keycloak.Group{},
		members: map[string][]keycloak.User{},
	}
}

func (s *stubDirectory) ListUsers(_ context.Context, _ string, _, _ int) ([]keycloak.User, error) {
	return s.users, nil
}

func (s *stubDirectory) CreateUser(_ context.Context, in keycloak.NewUser) (string, error) {
	s.createID = "u-1"
	return s.createID, nil
}

func (s *stubDirectory) UpdateUser(_ context.Context, _ string, _ keycloak.UserPatch) error {
	return nil
}

func (s *stubDirectory) ResetPassword(_ context.Context, id, _ string, _ bool) error {
	if id == "missing" {
		return keycloak.ErrNotFound
	}
	return nil
}

func (s *stubDirectory) ListGroups(_ context.Context, _ string) ([]keycloak.Group, error) {
	out := []keycloak.Group{}
	for _, g := range s.groups {
		out = append(out, g)
	}
	return out, nil
}

func (s *stubDirectory) CreateGroup(_ context.Context, name string) (string, error) {
	if name == "dup" {
		return "", keycloak.ErrConflict
	}
	s.groups["g-1"] = keycloak.Group{ID: "g-1", Name: name, Path: "/" + name}
	return "g-1", nil
}

func (s *stubDirectory) GetGroup(_ context.Context, id string) (keycloak.Group, error) {
	g, ok := s.groups[id]
	if !ok {
		return keycloak.Group{}, keycloak.ErrNotFound
	}
	return g, nil
}

func (s *stubDirectory) ListGroupMembers(_ context.Context, groupID string, _, _ int) ([]keycloak.User, error) {
	if _, ok := s.groups[groupID]; !ok {
		return nil, keycloak.ErrNotFound
	}
	return s.members[groupID], nil
}

func (s *stubDirectory) AddGroupMember(_ context.Context, groupID, userID string) error {
	g, ok := s.groups[groupID]
	if !ok {
		return keycloak.ErrNotFound
	}
	if strings.HasPrefix(g.Path, "/org:") {
		return errors.New("stub: guard must run before the client call")
	}
	s.members[groupID] = append(s.members[groupID], keycloak.User{ID: userID})
	return nil
}

func (s *stubDirectory) RemoveGroupMember(_ context.Context, groupID, userID string) error {
	if _, ok := s.groups[groupID]; !ok {
		return keycloak.ErrNotFound
	}
	return nil
}

func directoryRouter(t *testing.T) (*gin.Engine, *stubDirectory) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	stub := newStubDirectory()
	r := gin.New()
	NewKeycloakDirectoryHandler(stub).RegisterRoutes(r.Group("/api/v1"))
	return r, stub
}

// 端点契约钉死（同 platform_routes_test.go 的理由：文档与 console 引用路径，
// 拼错一个字符编译不报错，只会静默 404）。
func TestKeycloakDirectoryRoutesAreRegistered(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	NewKeycloakDirectoryHandler(nil).RegisterRoutes(r.Group("/api/v1"))

	got := map[string]bool{}
	for _, ri := range r.Routes() {
		got[ri.Method+" "+ri.Path] = true
	}
	want := []string{
		"GET /api/v1/keycloak/users",
		"POST /api/v1/keycloak/users",
		"PUT /api/v1/keycloak/users/:id",
		"PUT /api/v1/keycloak/users/:id/password",
		"GET /api/v1/keycloak/groups",
		"POST /api/v1/keycloak/groups",
		"GET /api/v1/keycloak/groups/:id",
		"GET /api/v1/keycloak/groups/:id/members",
		"PUT /api/v1/keycloak/groups/:id/members/:userId",
		"DELETE /api/v1/keycloak/groups/:id/members/:userId",
	}
	for _, w := range want {
		if !got[w] {
			t.Errorf("缺少目录代理路由 %s", w)
		}
	}
}

func TestCreateUser_ReturnsTemporaryPasswordOnce(t *testing.T) {
	r, _ := directoryRouter(t)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/keycloak/users",
		strings.NewReader(`{"username":"alice","email":"a@x.dev"}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
	var resp struct {
		Data createUserResp `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Data.TemporaryPassword == "" {
		t.Fatalf("expected temporaryPassword in the create response")
	}
	if !PasswordClassSatisfied(resp.Data.TemporaryPassword) {
		t.Fatalf("temporary password %q lacks required classes", resp.Data.TemporaryPassword)
	}
	if len(resp.Data.RequiredActions) != 1 || resp.Data.RequiredActions[0] != "UPDATE_PASSWORD" {
		t.Fatalf("expected UPDATE_PASSWORD required action, got %v", resp.Data.RequiredActions)
	}
}

func TestCreateUser_RejectsShortExplicitPassword(t *testing.T) {
	r, _ := directoryRouter(t)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/keycloak/users",
		strings.NewReader(`{"username":"alice","initialPassword":"short"}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for short password, got %d", w.Code)
	}
}

func TestCreateUser_RejectsEmptyUsername(t *testing.T) {
	r, _ := directoryRouter(t)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/keycloak/users", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for empty username, got %d", w.Code)
	}
}

func TestResetPassword_MapsNotFound(t *testing.T) {
	r, _ := directoryRouter(t)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/api/v1/keycloak/users/missing/password", nil)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", w.Code)
	}
}

func TestCreateGroup_ConflictAndOrgGuards(t *testing.T) {
	r, _ := directoryRouter(t)

	// 409 from the client sentinel
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/keycloak/groups", strings.NewReader(`{"name":"dup"}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	if w.Code != http.StatusConflict {
		t.Fatalf("expected 409 for duplicate group, got %d", w.Code)
	}

	// org: 前缀名拒绝 —— 组是 orgsvc 专属资源
	for _, name := range []string{"org", "org:team-a"} {
		w = httptest.NewRecorder()
		req = httptest.NewRequest(http.MethodPost, "/api/v1/keycloak/groups",
			strings.NewReader(`{"name":"`+name+`"}`))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		if w.Code != http.StatusForbidden {
			t.Fatalf("expected 403 for group name %q, got %d", name, w.Code)
		}
	}

	// 嵌套路径拒绝
	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/v1/keycloak/groups",
		strings.NewReader(`{"name":"parent/child"}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for nested group name, got %d", w.Code)
	}
}

// /org: 组对目录代理只读 —— 管理面侧（hub）强制，不依赖 UI 自觉。
func TestOrgCarrierGroupMembershipIsForbidden(t *testing.T) {
	r, stub := directoryRouter(t)
	stub.groups["g-org"] = keycloak.Group{ID: "g-org", Name: "org:team-a", Path: "/org:team-a"}

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/api/v1/keycloak/groups/g-org/members/u-9", nil)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 adding member to org group, got %d", w.Code)
	}

	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodDelete, "/api/v1/keycloak/groups/g-org/members/u-9", nil)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 removing member from org group, got %d", w.Code)
	}

	// 只读侧放行：GET 成员允许（选择器要展示组员）
	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/v1/keycloak/groups/g-org/members", nil)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 listing org group members, got %d", w.Code)
	}
}

func TestUpdateUser_NoEmptyPatch(t *testing.T) {
	r, _ := directoryRouter(t)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/api/v1/keycloak/users/u-1", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for empty patch, got %d", w.Code)
	}
}

func TestGenerateTemporaryPassword_IsRandomEnough(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 50; i++ {
		pw := GenerateTemporaryPassword()
		if len(pw) != 16 {
			t.Fatalf("expected 16-char password, got %d", len(pw))
		}
		if !PasswordClassSatisfied(pw) {
			t.Fatalf("password %q lacks required classes", pw)
		}
		if seen[pw] {
			t.Fatalf("password repeated across draws: %q", pw)
		}
		seen[pw] = true
	}
}

// 防呆：未知错误走 failPermission 的既有契约 —— 500 + 信封（error 文本，
// 无 ErrorCode；只有服务层构造的 *APIError 才带稳定码）。
func TestFailDirectory_UnknownErrorIsInternal(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	failDirectory(c, errors.New("boom"))
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 for unknown error, got %d", w.Code)
	}
	var env common.Envelope
	if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil {
		t.Fatalf("body is not an API envelope: %v", err)
	}
	if env.Error == "" {
		t.Fatalf("expected error text in envelope")
	}
}
