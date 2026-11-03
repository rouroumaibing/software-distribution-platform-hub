package session

import (
	"net/http"
	"time"

	"github.com/rouroumaibing/software-distribution-platform-hub/internal/credentials/codec"
)

// CookieName is the httpOnly cookie carrying the AES-GCM-encrypted session id.
const CookieName = "sdp_sess"

// CookieMaxAge is the cookie lifetime; matches the session row expiry. Both
// the cookie and the Get path's expires_at check enforce expiry (defense in
// depth — a stolen cookie past expiry is still useless even before GC).
const CookieMaxAge = 8 * time.Hour

// SetCookie issues the session cookie on the response. The value is the
// AES-GCM-encrypted session id (codec reuses CREDENTIAL_ENCRYPTION_KEY; in dev
// with no key the id is stored plaintext — dev-only). secure should be true in
// production (HTTPS); false is fine for local kind over plain HTTP.
func SetCookie(w http.ResponseWriter, sessionID string, secure bool) error {
	enc, err := codec.Encrypt(sessionID)
	if err != nil {
		return err
	}
	http.SetCookie(w, &http.Cookie{
		Name:     CookieName,
		Value:    enc,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   secure,
		MaxAge:   int(CookieMaxAge.Seconds()),
	})
	return nil
}

// ClearCookie expires the session cookie on the client (logout).
func ClearCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     CookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})
}

// ReadCookie extracts and decrypts the session id from the request. Returns
// empty string when absent or undecryptable — the caller treats that as
// unauthenticated (no session).
func ReadCookie(r *http.Request) string {
	c, err := r.Cookie(CookieName)
	if err != nil {
		return ""
	}
	id, err := codec.Decrypt(c.Value)
	if err != nil {
		return ""
	}
	return id
}
