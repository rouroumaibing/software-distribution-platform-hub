package keycloak

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// fakeKeycloak is a minimal in-memory Keycloak Admin API for tests. It tracks
// created groups by path and serves the token + groups endpoints.
type fakeKeycloak struct {
	tokenHits   int32
	postHits    int32
	getHits     int32
	groups      map[string]bool // path -> exists
	conflict    bool            // make POST return 409 (simulates concurrent create)
	createToken string
}

func newFake() *fakeKeycloak {
	return &fakeKeycloak{
		groups:      map[string]bool{},
		createToken: "test-access-token",
	}
}

func (f *fakeKeycloak) handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/protocol/openid-connect/token"):
			atomic.AddInt32(&f.tokenHits, 1)
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(tokenResp{AccessToken: f.createToken, ExpiresIn: 300})
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/groups"):
			atomic.AddInt32(&f.getHits, 1)
			// honor ?search=&exact= by matching the path segment
			q := r.URL.Query().Get("search")
			w.Header().Set("Content-Type", "application/json")
			if q != "" && f.groups["/"+q] {
				_ = json.NewEncoder(w).Encode([]map[string]string{{"name": q, "path": "/" + q}})
			} else {
				_ = json.NewEncoder(w).Encode([]map[string]string{})
			}
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/groups"):
			atomic.AddInt32(&f.postHits, 1)
			var body struct {
				Name string `json:"name"`
				Path string `json:"path"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			if f.groups[body.Path] {
				w.WriteHeader(http.StatusConflict)
				return
			}
			if f.conflict {
				f.groups[body.Path] = true
				w.WriteHeader(http.StatusConflict)
				return
			}
			f.groups[body.Path] = true
			w.WriteHeader(http.StatusCreated)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})
}

func newTestClient(f *fakeKeycloak) (*Client, *httptest.Server) {
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

func TestEnsureGroup_CreatesWhenMissing(t *testing.T) {
	f := newFake()
	c, srv := newTestClient(f)
	defer srv.Close()

	if err := c.EnsureGroup(context.Background(), "/org:team-a"); err != nil {
		t.Fatalf("EnsureGroup returned error: %v", err)
	}
	if !f.groups["/org:team-a"] {
		t.Fatalf("expected group /org:team-a to be created")
	}
	if f.postHits == 0 {
		t.Fatalf("expected a POST to create the group")
	}
}

func TestEnsureGroup_IdempotentWhenExists(t *testing.T) {
	f := newFake()
	f.groups["/org:team-a"] = true
	c, srv := newTestClient(f)
	defer srv.Close()

	if err := c.EnsureGroup(context.Background(), "/org:team-a"); err != nil {
		t.Fatalf("EnsureGroup returned error: %v", err)
	}
	if f.postHits != 0 {
		t.Fatalf("expected no POST when group already exists, got %d", f.postHits)
	}
	if f.getHits == 0 {
		t.Fatalf("expected a GET to check existence")
	}
}

func TestEnsureGroup_HandlesConflict(t *testing.T) {
	f := newFake()
	f.conflict = true // concurrent creation already landed -> 409
	c, srv := newTestClient(f)
	defer srv.Close()

	if err := c.EnsureGroup(context.Background(), "/org:team-a"); err != nil {
		t.Fatalf("conflict should be treated as success, got: %v", err)
	}
	if !f.groups["/org:team-a"] {
		t.Fatalf("expected group to exist after conflict handling")
	}
}

func TestEnsureGroup_CachesToken(t *testing.T) {
	f := newFake()
	c, srv := newTestClient(f)
	defer srv.Close()

	// Two calls should only hit the token endpoint once (cache + 300s skew).
	if err := c.EnsureGroup(context.Background(), "/org:one"); err != nil {
		t.Fatalf("first EnsureGroup: %v", err)
	}
	if err := c.EnsureGroup(context.Background(), "/org:two"); err != nil {
		t.Fatalf("second EnsureGroup: %v", err)
	}
	if f.tokenHits != 1 {
		t.Fatalf("expected token endpoint hit once due to caching, got %d", f.tokenHits)
	}
}

func TestEnsureGroup_EmptyPath(t *testing.T) {
	f := newFake()
	c, srv := newTestClient(f)
	defer srv.Close()
	if err := c.EnsureGroup(context.Background(), ""); err == nil {
		t.Fatalf("expected error for empty group path")
	}
}
