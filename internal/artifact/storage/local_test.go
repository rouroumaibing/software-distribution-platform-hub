package storage

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func newTestLocal(t *testing.T) *Local {
	t.Helper()
	gin.SetMode(gin.TestMode)
	l, err := NewLocal(filepath.Join(t.TempDir(), "artifacts"), "test-secret", "http://hub.test")
	if err != nil {
		t.Fatalf("NewLocal: %v", err)
	}
	return l
}

func serve(l *Local, method, requestURI string, body io.Reader) *httptest.ResponseRecorder {
	r := gin.New()
	r.Any(RoutePrefixPattern(), l.GinHandler())
	w := httptest.NewRecorder()
	req := httptest.NewRequest(method, requestURI, body)
	r.ServeHTTP(w, req)
	return w
}

func setQuery(u *url.URL, k, v string) {
	q := u.Query()
	q.Set(k, v)
	u.RawQuery = q.Encode()
}

func TestLocalPresignRoundTrip(t *testing.T) {
	l := newTestLocal(t)

	upURL, err := l.PresignUpload("components/abc/v1.0.0/app.tar.gz", time.Minute)
	if err != nil {
		t.Fatalf("PresignUpload: %v", err)
	}
	if !strings.HasPrefix(upURL, "http://hub.test"+storageRoute) {
		t.Fatalf("upload URL has unexpected base: %s", upURL)
	}

	// Upload through the signed URL (path-escape round trip).
	u, _ := url.Parse(upURL)
	if w := serve(l, http.MethodPut, u.RequestURI(), bytes.NewReader([]byte("hello artifact"))); w.Code != http.StatusOK {
		t.Fatalf("upload status = %d, body=%s", w.Code, w.Body.String())
	}

	// Download through the signed URL and compare bytes.
	dlURL, err := l.PresignDownload("components/abc/v1.0.0/app.tar.gz", time.Minute)
	if err != nil {
		t.Fatalf("PresignDownload: %v", err)
	}
	u, _ = url.Parse(dlURL)
	w := serve(l, http.MethodGet, u.RequestURI(), nil)
	if w.Code != http.StatusOK {
		t.Fatalf("download status = %d, body=%s", w.Code, w.Body.String())
	}
	if got := w.Body.String(); got != "hello artifact" {
		t.Fatalf("downloaded %q", got)
	}

	// Delete removes the object.
	if err := l.Delete("components/abc/v1.0.0/app.tar.gz"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
}

func TestLocalVerifyRejects(t *testing.T) {
	l := newTestLocal(t)

	// Traversal keys must be rejected at mint time.
	if _, err := l.PresignUpload("../escape", time.Minute); err == nil {
		t.Fatalf("PresignUpload accepted traversal key")
	}

	// Wrong method: a PUT signature must not authorize a GET.
	putURL, err := l.PresignUpload("k.txt", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	u, _ := url.Parse(putURL)
	if w := serve(l, http.MethodGet, u.RequestURI(), nil); w.Code == http.StatusOK {
		t.Fatalf("GET with PUT signature accepted")
	}

	// Tampered signature.
	dlURL, err := l.PresignDownload("k.txt", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	u, _ = url.Parse(dlURL)
	setQuery(u, "sig", "deadbeef")
	if w := serve(l, http.MethodGet, u.RequestURI(), nil); w.Code != http.StatusForbidden {
		t.Fatalf("tampered signature: status = %d", w.Code)
	}

	// Expired signature.
	u, _ = url.Parse(dlURL)
	setQuery(u, "exp", "1")
	if w := serve(l, http.MethodGet, u.RequestURI(), nil); w.Code != http.StatusForbidden {
		t.Fatalf("expired signature: status = %d", w.Code)
	}
}

func TestValidateKey(t *testing.T) {
	valid := []string{"a", "a/b/c.tar.gz", "components/abc v1/app~1.bin"}
	for _, k := range valid {
		if err := ValidateKey(k); err != nil {
			t.Errorf("ValidateKey(%q) = %v, want nil", k, err)
		}
	}
	invalid := []string{"", "/abs", "../up", "a/../b", "a//b", "./a", strings.Repeat("x", 513)}
	for _, k := range invalid {
		if err := ValidateKey(k); err == nil {
			t.Errorf("ValidateKey(%q) = nil, want error", k)
		}
	}
}

func TestUploadHasNoLeftoverTemp(t *testing.T) {
	l := newTestLocal(t)
	key := "components/111/2.0.0/server"
	up, err := l.PresignUpload(key, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	u, _ := url.Parse(up)
	if w := serve(l, http.MethodPut, u.RequestURI(), strings.NewReader("payload")); w.Code != 200 {
		t.Fatalf("upload: %d", w.Code)
	}
	err = filepath.Walk(l.root, func(p string, info os.FileInfo, err error) error {
		if err == nil && strings.HasSuffix(p, ".part") {
			t.Errorf("leftover temp file: %s", p)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
