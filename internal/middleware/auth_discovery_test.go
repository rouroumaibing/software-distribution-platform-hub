package middleware

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
)

// roundTripperFunc lets a test control the discovery HTTP response sequence.
type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func discoveryResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Status:     http.StatusText(status),
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
		Request:    &http.Request{},
	}
}

const validDiscovery = `{
  "issuer": "https://issuer.example/realms/sdp",
  "authorization_endpoint": "https://issuer.example/realms/sdp/protocol/openid-connect/auth",
  "token_endpoint": "https://issuer.example/realms/sdp/protocol/openid-connect/token",
  "jwks_uri": "https://issuer.example/realms/sdp/protocol/openid-connect/certs",
  "id_token_signing_alg_values_supported": ["RS256"]
}`

// A transient 503 followed by a valid document must converge on the second
// attempt — the exact failure mode seen when Keycloak boots behind hub.
func TestDiscoverProviderRetriesTransient503(t *testing.T) {
	calls := 0
	client := &http.Client{Transport: roundTripperFunc(func(*http.Request) (*http.Response, error) {
		calls++
		if calls == 1 {
			return discoveryResponse(http.StatusServiceUnavailable, "warming up"), nil
		}
		return discoveryResponse(http.StatusOK, validDiscovery), nil
	})}
	ctx := oidc.ClientContext(context.Background(), client)
	_, err := discoverProvider(ctx, AuthConfig{
		IssuerURL:         "https://issuer.example/realms/sdp",
		DiscoveryAttempts: 3,
		DiscoveryInterval: time.Millisecond,
	})
	if err != nil {
		t.Fatalf("discoverProvider: %v", err)
	}
	if calls != 2 {
		t.Errorf("HTTP calls = %d, want 2 (one retry)", calls)
	}
}

// A wrong issuer document is a permanent misconfiguration: go-oidc returns
// *IssuerMismatchError, which must abort without burning the retry budget.
func TestDiscoverProviderFailsFastOnIssuerMismatch(t *testing.T) {
	calls := 0
	client := &http.Client{Transport: roundTripperFunc(func(*http.Request) (*http.Response, error) {
		calls++
		return discoveryResponse(http.StatusOK, `{
			"issuer": "https://different.example/realms/other",
			"authorization_endpoint": "https://different.example/auth",
			"token_endpoint": "https://different.example/token",
			"jwks_uri": "https://different.example/certs",
			"id_token_signing_alg_values_supported": ["RS256"]
		}`), nil
	})}
	ctx := oidc.ClientContext(context.Background(), client)
	_, err := discoverProvider(ctx, AuthConfig{
		IssuerURL:         "https://issuer.example/realms/sdp",
		DiscoveryAttempts: 5,
		DiscoveryInterval: time.Millisecond,
	})
	var mismatch *oidc.IssuerMismatchError
	if !errors.As(err, &mismatch) {
		t.Fatalf("err = %v, want *oidc.IssuerMismatchError", err)
	}
	if calls != 1 {
		t.Errorf("HTTP calls = %d, want 1 (no retry on permanent error)", calls)
	}
}

// Persistent transient errors must stop at the attempt budget and surface the
// last error — bounded retry, never an infinite hang.
func TestDiscoverProviderExhaustsBudget(t *testing.T) {
	calls := 0
	client := &http.Client{Transport: roundTripperFunc(func(*http.Request) (*http.Response, error) {
		calls++
		return discoveryResponse(http.StatusServiceUnavailable, "still down"), nil
	})}
	ctx := oidc.ClientContext(context.Background(), client)
	_, err := discoverProvider(ctx, AuthConfig{
		IssuerURL:         "https://issuer.example/realms/sdp",
		DiscoveryAttempts: 3,
		DiscoveryInterval: time.Millisecond,
	})
	if err == nil {
		t.Fatal("expected error after exhausting attempts")
	}
	if calls != 3 {
		t.Errorf("HTTP calls = %d, want 3 (attempt budget)", calls)
	}
	if !strings.Contains(err.Error(), "still down") {
		t.Errorf("err = %v, want last transient error body surfaced", err)
	}
}

// Context cancellation interrupts the backoff wait instead of sleeping through it.
func TestDiscoverProviderRespectsContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(oidc.ClientContext(context.Background(), &http.Client{
		Transport: roundTripperFunc(func(*http.Request) (*http.Response, error) {
			return discoveryResponse(http.StatusServiceUnavailable, "down"), nil
		}),
	}))
	cancel()
	_, err := discoverProvider(ctx, AuthConfig{
		IssuerURL:         "https://issuer.example/realms/sdp",
		DiscoveryAttempts: 5,
		DiscoveryInterval: time.Second,
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}
