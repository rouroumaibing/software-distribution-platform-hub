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

// ObjectInfo is one object as seen in the store.
type ObjectInfo struct {
	Key  string
	Size int64
}

// Enumerator is the **read-only listing** surface, used solely by the periodic
// orphan reconciliation job (backlog B-16 对账).
//
// 刻意与 Client 分开（接口隔离）：
//   - 下载 / 上传 / 删除是每请求热路径，驱动必须实现；列出整桶是低频运维动作，
//     且成本随桶大小增长 —— 不该让每个 Client 实现都被迫背上它；
//   - 更重要的是**不能假装支持**：某个驱动若无法列举，"对账结果 = 0 个孤儿"
//     会与"根本查不了"混淆。调用方用类型断言判断，不支持时如实报告"跳过"。
type Enumerator interface {
	// ListObjects returns objects whose key starts with prefix ("", the
	// default, means the whole store). Implementations must stop early once
	// limit is reached and signal it, so a huge bucket cannot exhaust memory.
	ListObjects(prefix string, limit int) (objects []ObjectInfo, truncated bool, err error)
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
