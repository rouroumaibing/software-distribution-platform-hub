// Package codec encrypts credential secret values at rest.
//
// Why this exists (steelman, 2026-09-23): Credential.Value holds high-value
// secrets (kubeconfig / ssh key / password). Storing them as plaintext in the
// DB means anyone with DB read access (a SQL injection, a leaked backup, a
// Shared dev cluster) gets every cluster credential. AES-GCM with a key from
// CREDENTIAL_ENCRYPTION_KEY is a standard, dependency-free envelope that needs
// no new infra (no Vault, no client-go — the hub has neither).
//
// Why not "store only a reference name" (false-steelman): that needs an
// external secret store this project does not run. So envelope encryption in
// the DB column is the pragmatic middle ground.
//
// Reversibility (the plan's "可逆方案" requirement):
//   - Encryption is purely a storage detail, encapsulated in the service layer.
//     In memory the model always holds plaintext; only the persisted column is
//     ciphertext.
//   - The ciphertext is prefixed "enc:v1:" so plaintext and ciphertext rows
//     coexist. Decrypt treats any value WITHOUT the prefix as plaintext, so
//     turning encryption on later never breaks existing rows, and turning it
//     off (or losing the column) degrades to plaintext rather than corrupting
//     data.
//   - No key configured (dev) => store plaintext with a one-time warning.
//     Symmetric: with no key, decrypt returns the stored value as-is.
//
// Trade-off: if a key IS configured and later lost, encrypted rows are
// unrecoverable. That is inherent to encryption; the key is an operational
// secret the deployer must back up (documented in KEYCLOAK.md / values).
package codec

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"io"
	"os"
	"strings"
	"sync"

	applog "github.com/rouroumaibing/software-distribution-platform-hub/internal/common/logger"
)

const prefix = "enc:v1:"

var (
	key     []byte
	keyOnce sync.Once
	warned  bool
)

// Key returns the configured encryption key (raw bytes; 32 => AES-256). An
// empty result means encryption is disabled (dev / no secret configured).
// Read once and cached so a hot path never re-reads the environment.
func Key() []byte {
	keyOnce.Do(func() {
		if raw := os.Getenv("CREDENTIAL_ENCRYPTION_KEY"); raw != "" {
			key = []byte(raw)
		}
	})
	return key
}

// Encrypt returns the ciphertext form of plaintext. With no key configured it
// returns plaintext unchanged (dev mode) after logging a one-time warning.
func Encrypt(plaintext string) (string, error) {
	k := Key()
	if len(k) == 0 {
		if !warned {
			applog.Warnf("credentials: CREDENTIAL_ENCRYPTION_KEY not set — storing credential value in plaintext (dev mode)")
			warned = true
		}
		return plaintext, nil
	}
	block, err := aes.NewCipher(k)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	sealed := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return prefix + base64.StdEncoding.EncodeToString(sealed), nil
}

// Decrypt reverses Encrypt. A value without the prefix is treated as plaintext
// (legacy / dev row) and returned unchanged, so mixed storage is safe.
func Decrypt(stored string) (string, error) {
	if !strings.HasPrefix(stored, prefix) {
		return stored, nil
	}
	k := Key()
	if len(k) == 0 {
		return "", errors.New("credentials: cannot decrypt — CREDENTIAL_ENCRYPTION_KEY not set")
	}
	b, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(stored, prefix))
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(k)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	ns := gcm.NonceSize()
	if len(b) < ns {
		return "", errors.New("credentials: ciphertext too short")
	}
	nonce, ct := b[:ns], b[ns:]
	pt, err := gcm.Open(nil, nonce, ct, nil)
	if err != nil {
		return "", err
	}
	return string(pt), nil
}
