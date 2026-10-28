package keycloak

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeDirectory is a self-contained in-memory Keycloak Admin API for the
// directory proxy tests: users (create/update/reset-password), groups
// (create/get/members). Independent of fakeKeycloak so the provisioning tests
// keep their exact semantics.
type fakeDirectory struct {
	mu           sync.Mutex
	users        map[string]map[string]any // id -> representation
	groups       map[string]Group          // id -> group
	members      map[string]map[string]bool
	seq          int
	conflictUser bool // POST /users returns 409
}

func newFakeDirectory() *fakeDirectory {
	return &fakeDirectory{
		users:   map[string]map[string]any{},
		groups:  map[string]Group{},
		members: map[string]map[string]bool{},
	}
}

func (f *fakeDirectory) handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/protocol/openid-connect/token"):
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(tokenResp{AccessToken: "test-token", ExpiresIn: 300})
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/users"):
			f.mu.Lock()
			defer f.mu.Unlock()
			out := []User{}
			search := r.URL.Query().Get("search")
			for _, rep := range f.users {
				u := User{Username: rep["username"].(string), Enabled: rep["enabled"].(bool)}
				out = append(out, u)
			}
			if search != "" {
				filtered := out[:0]
				for _, u := range out {
					if strings.Contains(u.Username, search) {
						filtered = append(filtered, u)
					}
				}
				out = filtered
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(out)
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/users"):
			f.mu.Lock()
			defer f.mu.Unlock()
			if f.conflictUser {
				w.WriteHeader(http.StatusConflict)
				return
			}
			var rep map[string]any
			_ = json.NewDecoder(r.Body).Decode(&rep)
			f.seq++
			id := "u-" + itoa(f.seq)
			rep["id"] = id
			f.users[id] = rep
			w.Header().Set("Location", "/admin/realms/sdp/users/"+id)
			w.WriteHeader(http.StatusCreated)
		case strings.Contains(r.URL.Path, "/reset-password"):
			f.mu.Lock()
			defer f.mu.Unlock()
			parts := strings.Split(r.URL.Path, "/")
			if _, ok := f.users[parts[len(parts)-2]]; !ok {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			w.WriteHeader(http.StatusNoContent)
		case strings.HasPrefix(strings.TrimPrefix(r.URL.Path, "/admin/realms/sdp/users/"), "u-"):
			f.mu.Lock()
			defer f.mu.Unlock()
			id := strings.TrimPrefix(r.URL.Path, "/admin/realms/sdp/users/")
			if _, ok := f.users[id]; !ok {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			var rep map[string]any
			_ = json.NewDecoder(r.Body).Decode(&rep)
			for k, v := range rep {
				f.users[id][k] = v
			}
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/groups"):
			f.mu.Lock()
			defer f.mu.Unlock()
			out := []Group{}
			for _, g := range f.groups {
				out = append(out, g)
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(out)
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/groups"):
			f.mu.Lock()
			defer f.mu.Unlock()
			var body map[string]string
			_ = json.NewDecoder(r.Body).Decode(&body)
			for _, g := range f.groups {
				if g.Path == body["path"] {
					w.WriteHeader(http.StatusConflict)
					return
				}
			}
			f.seq++
			id := "g-" + itoa(f.seq)
			f.groups[id] = Group{ID: id, Name: body["name"], Path: body["path"]}
			w.Header().Set("Location", "/admin/realms/sdp/groups/"+id)
			w.WriteHeader(http.StatusCreated)
		case strings.Contains(r.URL.Path, "/groups/g-") && strings.Contains(r.URL.Path, "/members"):
			f.handleMembers(w, r)
		case strings.HasPrefix(strings.TrimPrefix(r.URL.Path, "/admin/realms/sdp/groups/"), "g-"):
			f.mu.Lock()
			defer f.mu.Unlock()
			id := strings.Trim(strings.TrimPrefix(r.URL.Path, "/admin/realms/sdp/groups/"), "/")
			g, ok := f.groups[id]
			if !ok {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(g)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})
}

func (f *fakeDirectory) handleMembers(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	rest := strings.TrimPrefix(r.URL.Path, "/admin/realms/sdp/groups/")
	parts := strings.Split(strings.Trim(rest, "/"), "/") // [gid, "members", uid?]
	gid := parts[0]
	if _, ok := f.groups[gid]; !ok {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	if len(parts) < 3 {
		out := []User{}
		for uid := range f.members[gid] {
			if rep, ok := f.users[uid]; ok {
				out = append(out, User{ID: uid, Username: rep["username"].(string)})
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(out)
		return
	}
	uid := parts[2]
	switch r.Method {
	case http.MethodPut:
		if _, ok := f.users[uid]; !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if f.members[gid] == nil {
			f.members[gid] = map[string]bool{}
		}
		f.members[gid][uid] = true
		w.WriteHeader(http.StatusNoContent)
	case http.MethodDelete:
		if !f.members[gid][uid] {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		delete(f.members[gid], uid)
		w.WriteHeader(http.StatusNoContent)
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}

func newDirectoryClient(f *fakeDirectory) (*Client, *httptest.Server) {
	srv := httptest.NewServer(f.handler())
	c := &Client{
		AdminBaseURL: srv.URL + "/admin/realms/sdp",
		TokenURL:     srv.URL + "/realms/sdp/protocol/openid-connect/token",
		ClientID:     "sdp-backend",
		ClientSecret: "secret",
		HTTP:         &http.Client{Timeout: 5 * time.Second},
	}
	return c, srv
}

func TestCreateUser_ReturnsIDFromLocation(t *testing.T) {
	f := newFakeDirectory()
	c, srv := newDirectoryClient(f)
	defer srv.Close()

	id, err := c.CreateUser(context.Background(), NewUser{Username: "alice", Enabled: true, RequiredActions: []string{"UPDATE_PASSWORD"}})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if id == "" {
		t.Fatalf("expected non-empty user id")
	}
	if _, ok := f.users[id]; !ok {
		t.Fatalf("user %s not recorded in fake", id)
	}
}

func TestCreateUser_Conflict(t *testing.T) {
	f := newFakeDirectory()
	f.conflictUser = true
	c, srv := newDirectoryClient(f)
	defer srv.Close()

	if _, err := c.CreateUser(context.Background(), NewUser{Username: "bob"}); err != ErrConflict {
		t.Fatalf("expected ErrConflict, got %v", err)
	}
}

func TestCreateUser_EmptyUsername(t *testing.T) {
	c, srv := newDirectoryClient(newFakeDirectory())
	defer srv.Close()
	if _, err := c.CreateUser(context.Background(), NewUser{}); err == nil {
		t.Fatalf("expected error for empty username")
	}
}

func TestListUsers_Search(t *testing.T) {
	f := newFakeDirectory()
	c, srv := newDirectoryClient(f)
	defer srv.Close()
	if _, err := c.CreateUser(context.Background(), NewUser{Username: "alice"}); err != nil {
		t.Fatalf("seed alice: %v", err)
	}
	if _, err := c.CreateUser(context.Background(), NewUser{Username: "carol"}); err != nil {
		t.Fatalf("seed carol: %v", err)
	}

	users, err := c.ListUsers(context.Background(), "ali", 0, 0)
	if err != nil {
		t.Fatalf("ListUsers: %v", err)
	}
	if len(users) != 1 || users[0].Username != "alice" {
		t.Fatalf("expected only alice, got %+v", users)
	}
	all, err := c.ListUsers(context.Background(), "", 0, 0)
	if err != nil {
		t.Fatalf("ListUsers all: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("expected 2 users, got %d", len(all))
	}
}

func TestUpdateUser_AndResetPassword(t *testing.T) {
	f := newFakeDirectory()
	c, srv := newDirectoryClient(f)
	defer srv.Close()
	id, _ := c.CreateUser(context.Background(), NewUser{Username: "dave", Enabled: true})

	enabled := false
	if err := c.UpdateUser(context.Background(), id, UserPatch{Enabled: &enabled}); err != nil {
		t.Fatalf("UpdateUser: %v", err)
	}
	if f.users[id]["enabled"].(bool) {
		t.Fatalf("expected enabled=false after patch")
	}

	if err := c.ResetPassword(context.Background(), id, "temp-pass-1", true); err != nil {
		t.Fatalf("ResetPassword: %v", err)
	}
	// 404 mapping
	if err := c.ResetPassword(context.Background(), "u-999", "x", true); err != ErrNotFound {
		t.Fatalf("expected ErrNotFound for reset on missing user, got %v", err)
	}
	if err := c.UpdateUser(context.Background(), "u-999", UserPatch{Enabled: &enabled}); err != ErrNotFound {
		t.Fatalf("expected ErrNotFound for update on missing user, got %v", err)
	}
}

func TestGroupLifecycle(t *testing.T) {
	f := newFakeDirectory()
	c, srv := newDirectoryClient(f)
	defer srv.Close()

	gid, err := c.CreateGroup(context.Background(), "release-managers")
	if err != nil || gid == "" {
		t.Fatalf("CreateGroup: id=%q err=%v", gid, err)
	}
	if _, err := c.CreateGroup(context.Background(), "release-managers"); err != ErrConflict {
		t.Fatalf("expected ErrConflict on duplicate group, got %v", err)
	}

	g, err := c.GetGroup(context.Background(), gid)
	if err != nil {
		t.Fatalf("GetGroup: %v", err)
	}
	if g.Path != "/release-managers" {
		t.Fatalf("unexpected path %q", g.Path)
	}
	if _, err := c.GetGroup(context.Background(), "g-999"); err != ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}

	uid, _ := c.CreateUser(context.Background(), NewUser{Username: "erin", Enabled: true})
	if err := c.AddGroupMember(context.Background(), gid, uid); err != nil {
		t.Fatalf("AddGroupMember: %v", err)
	}
	members, err := c.ListGroupMembers(context.Background(), gid, 0, 0)
	if err != nil {
		t.Fatalf("ListGroupMembers: %v", err)
	}
	if len(members) != 1 || members[0].ID != uid {
		t.Fatalf("expected erin as member, got %+v", members)
	}
	if err := c.AddGroupMember(context.Background(), gid, "u-999"); err != ErrNotFound {
		t.Fatalf("expected ErrNotFound adding missing user, got %v", err)
	}
	if err := c.RemoveGroupMember(context.Background(), gid, uid); err != nil {
		t.Fatalf("RemoveGroupMember: %v", err)
	}
	if err := c.RemoveGroupMember(context.Background(), gid, uid); err != ErrNotFound {
		t.Fatalf("expected ErrNotFound removing absent member, got %v", err)
	}
}

func TestEnsureGroup_StillTreatsConflictAsSuccess(t *testing.T) {
	// Guard the refactor: doCreateGroup must keep EnsureGroup's 409-as-success
	// contract even though directory CreateGroup surfaces ErrConflict.
	f := newFakeDirectory()
	c, srv := newDirectoryClient(f)
	defer srv.Close()
	f.groups["g-x"] = Group{ID: "g-x", Name: "org:team-a", Path: "/org:team-a"}
	if err := c.EnsureGroup(context.Background(), "/org:team-a"); err != nil {
		t.Fatalf("EnsureGroup on existing group should succeed, got %v", err)
	}
}
