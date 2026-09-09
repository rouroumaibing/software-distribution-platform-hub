package storage

import (
	"context"
	"net/url"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// S3 talks to any S3-compatible object store (MinIO / AWS S3 / OSS S3
// gateway) via minio-go. Presigned URLs let the console download and archive
// tasks upload directly against the store — the hub only mints the URLs.
type S3 struct {
	cli    *minio.Client
	bucket string
}

// S3Config carries the connection settings from hub configuration.
type S3Config struct {
	Endpoint         string // host:port, e.g. minio.infra.svc:9000
	AccessKey        string
	SecretKey        string
	Bucket           string
	Region           string
	Secure           bool // true for https
	SessionToken     string
	LookupIPTypeIPv4 bool // MinIO behind DNS-only k8s services needs this
}

// NewS3 validates the config and establishes a client (no network I/O —
// credential/endpoint sanity is checked lazily by the first request).
func NewS3(cfg S3Config) (*S3, error) {
	opts := &minio.Options{
		Creds:  credentials.NewStaticV4(cfg.AccessKey, cfg.SecretKey, cfg.SessionToken),
		Secure: cfg.Secure,
		Region: cfg.Region,
	}
	cli, err := minio.New(cfg.Endpoint, opts)
	if err != nil {
		return nil, err
	}
	return &S3{cli: cli, bucket: cfg.Bucket}, nil
}

func (s *S3) PresignDownload(key string, expiry time.Duration) (string, error) {
	if err := ValidateKey(key); err != nil {
		return "", err
	}
	u, err := s.cli.PresignedGetObject(context.Background(), s.bucket, key, expiry, url.Values{})
	if err != nil {
		return "", err
	}
	return u.String(), nil
}

func (s *S3) PresignUpload(key string, expiry time.Duration) (string, error) {
	if err := ValidateKey(key); err != nil {
		return "", err
	}
	u, err := s.cli.PresignedPutObject(context.Background(), s.bucket, key, expiry)
	if err != nil {
		return "", err
	}
	return u.String(), nil
}

func (s *S3) Delete(key string) error {
	return s.cli.RemoveObject(context.Background(), s.bucket, key, minio.RemoveObjectOptions{})
}
