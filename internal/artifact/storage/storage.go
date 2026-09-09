// Package storage abstracts the object store behind the artifact library.
// Two drivers are provided:
//
//   - S3: any S3-compatible store (MinIO, AWS S3, Aliyun OSS via its S3
//     gateway). Downloads/uploads are pre-signed so bytes never flow through
//     the hub — the console and archive tasks talk to the store directly.
//   - Local: a dev-only disk backend. There is no presigning in a filesystem,
//     so "signed URLs" point back at the hub, which authenticates them with
//     an HMAC token and streams the file itself.
//
// A nil Client (no ARTIFACT_STORE_DRIVER configured) is valid: the artifact
// endpoints still list metadata, but download/upload URL generation fails
// with a clear "not configured" error.
package storage

import (
	"errors"
	"time"
)

// ErrNotConfigured is returned when no object store driver is configured.
var ErrNotConfigured = errors.New("storage: object storage not configured (set ARTIFACT_STORE_DRIVER)")

// ErrBadKey is returned for storage keys that could escape their prefix.
var ErrBadKey = errors.New("storage: invalid storage key")

// Client is the narrow surface the artifact feature needs. Expiry applies to
// the generated URL, not to the object itself.
type Client interface {
	// PresignDownload returns a short-lived GET URL for the object.
	PresignDownload(key string, expiry time.Duration) (string, error)
	// PresignUpload returns a short-lived PUT URL archive tasks can upload to.
	PresignUpload(key string, expiry time.Duration) (string, error)
	// Delete removes the object (best effort — DB row deletion proceeds even
	// if this fails).
	Delete(key string) error
}

// ValidateKey rejects keys that try to escape their prefix or abuse length.
// All drivers share this rule so artifacts can switch drivers freely.
func ValidateKey(key string) error {
	if key == "" || len(key) > 512 {
		return ErrBadKey
	}
	if key[0] == '/' {
		return ErrBadKey
	}
	for _, part := range splitPath(key) {
		if part == "" || part == "." || part == ".." {
			return ErrBadKey
		}
	}
	return nil
}

func splitPath(key string) []string {
	var parts []string
	start := 0
	for i := 0; i < len(key); i++ {
		if key[i] == '/' {
			parts = append(parts, key[start:i])
			start = i + 1
		}
	}
	return append(parts, key[start:])
}
