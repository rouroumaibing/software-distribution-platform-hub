package storage

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

// Local is a dev-only disk backend. A filesystem has no presigning, so the
// "signed URLs" it mints point back at the hub: an HMAC token authenticates
// the request and the hub streams the file itself (GET) or accepts an upload
// (PUT). Same token scheme as the S3 driver's time-limited signatures —
// short expiry, bound to method + key.
type Local struct {
	root       string // absolute dir all keys live under
	secret     []byte // HMAC signing secret
	publicBase string // e.g. http://localhost:8080 — base of the minted URLs
}

// NewLocal creates the root directory if missing.
func NewLocal(root, signSecret, publicBase string) (*Local, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(abs, 0o755); err != nil {
		return nil, err
	}
	return &Local{root: abs, secret: []byte(signSecret), publicBase: strings.TrimRight(publicBase, "/")}, nil
}

const storageRoute = "/api/v1/storage/"

// RoutePrefixPattern is the gin wildcard pattern the local driver's handler
// must be mounted under; it must match storageRoute.
func RoutePrefixPattern() string { return storageRoute + "*key" }

func (l *Local) sign(method, key string, exp int64) string {
	payload := fmt.Sprintf("%d|%s|%s", exp, method, key)
	mac := hmac.New(sha256.New, l.secret)
	mac.Write([]byte(payload))
	return hex.EncodeToString(mac.Sum(nil))
}

func (l *Local) presign(method, key string, expiry time.Duration) (string, error) {
	if err := ValidateKey(key); err != nil {
		return "", err
	}
	exp := time.Now().Add(expiry).Unix()
	sig := l.sign(method, key, exp)
	return fmt.Sprintf("%s%s%s?method=%s&exp=%d&sig=%s",
		l.publicBase, storageRoute, url.PathEscape(key), method, exp, sig), nil
}

func (l *Local) PresignDownload(key string, expiry time.Duration) (string, error) {
	return l.presign(http.MethodGet, key, expiry)
}

func (l *Local) PresignUpload(key string, expiry time.Duration) (string, error) {
	return l.presign(http.MethodPut, key, expiry)
}

func (l *Local) Delete(key string) error {
	if err := ValidateKey(key); err != nil {
		return err
	}
	err := os.Remove(filepath.Join(l.root, filepath.FromSlash(key)))
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// resolve maps a request key to a path inside root, rejecting traversal.
func (l *Local) resolve(key string) (string, error) {
	if err := ValidateKey(key); err != nil {
		return "", err
	}
	p := filepath.Clean(filepath.Join(l.root, filepath.FromSlash(key)))
	if !strings.HasPrefix(p, l.root+string(filepath.Separator)) {
		return "", ErrBadKey
	}
	return p, nil
}

// verify checks method/expiry/signature for a signed storage request.
func (l *Local) verify(c *gin.Context, key string) bool {
	exp, err := strconv.ParseInt(c.Query("exp"), 10, 64)
	if err != nil || time.Now().Unix() > exp {
		return false
	}
	sig := c.Query("sig")
	if sig == "" {
		return false
	}
	want := l.sign(c.Request.Method, key, exp)
	return hmac.Equal([]byte(want), []byte(sig))
}

// GinHandler serves the signed storage route (GET download / PUT upload).
// It is mounted OUTSIDE the auth middleware — the HMAC token is the
// authorization, so browser downloads (no Authorization header) work.
func (l *Local) GinHandler() gin.HandlerFunc {
	return func(c *gin.Context) {
		// gin's *key wildcard includes the leading "/", but signatures are
		// computed over the bare key — strip it back off.
		key := strings.TrimPrefix(c.Param("key"), "/")
		if raw, err := url.PathUnescape(key); err == nil {
			key = raw
		}
		if err := ValidateKey(key); err != nil {
			c.String(http.StatusBadRequest, "invalid key")
			return
		}
		if !l.verify(c, key) {
			c.String(http.StatusForbidden, "invalid or expired signature")
			return
		}
		path, err := l.resolve(key)
		if err != nil {
			c.String(http.StatusBadRequest, "invalid key")
			return
		}

		switch c.Request.Method {
		case http.MethodGet:
			f, err := os.Open(path)
			if err != nil {
				c.String(http.StatusNotFound, "object not found")
				return
			}
			defer f.Close()
			st, err := f.Stat()
			if err != nil {
				c.String(http.StatusInternalServerError, "stat failed")
				return
			}
			c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filepath.Base(path)))
			http.ServeContent(c.Writer, c.Request, filepath.Base(path), st.ModTime(), f)
		case http.MethodPut:
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				c.String(http.StatusInternalServerError, "mkdir failed")
				return
			}
			tmp := path + ".part"
			if err := saveRequest(c, tmp); err != nil {
				c.String(http.StatusInternalServerError, "write failed")
				return
			}
			if err := os.Rename(tmp, path); err != nil {
				c.String(http.StatusInternalServerError, "rename failed")
				return
			}
			c.Status(http.StatusOK)
		default:
			c.String(http.StatusMethodNotAllowed, "method not allowed")
		}
	}
}

func saveRequest(c *gin.Context, path string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.ReadFrom(c.Request.Body)
	return err
}
