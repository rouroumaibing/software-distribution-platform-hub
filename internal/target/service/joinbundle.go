package service

import (
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"unicode/utf8"

	"github.com/google/uuid"
)

// ErrJoinBundleUnavailable marks a join-bundle request that cannot be served
// because the hub lacks the deployment facts it needs (no public URL, or no
// readable CA file). The handler maps it to 501 so callers see an explicit
// "not configured" instead of a misleading 500 or an empty bundle (ADR-0002).
var ErrJoinBundleUnavailable = errors.New("join-bundle unavailable: HUB_PUBLIC_URL or TLS CA not configured")

// JoinBundle is everything a runner installer needs to bootstrap one target
// (ADR-0002): the externally reachable URLs, the CA trust root plus its
// RFC7469 SPKI SHA256 fingerprint (kubeadm-compatible self-verification), and
// the per-target one-time enroll token (§9.9 semantics).
type JoinBundle struct {
	TargetName    string `json:"targetName"`
	GatewayURL    string `json:"gatewayUrl"`
	APIBaseURL    string `json:"apiBaseUrl"`
	CAPem         string `json:"caPem"`
	CAFingerprint string `json:"caFingerprint"`
	EnrollToken   string `json:"enrollToken"`
	ExpiresIn     string `json:"expiresIn"`
	RunnerVersion string `json:"runnerVersion"`
}

// JoinBundleIssuer mints join bundles on the user's RBAC-authenticated
// request. It is served by POST /targets/:id/join-bundle and consumed on an
// already-trusted channel (operator session / provisioning pipeline) — never
// by the target machine itself, which cannot trust the hub before bootstrap.
type JoinBundleIssuer struct {
	targets       *TargetService
	publicURL     string
	caFile        string
	runnerVersion string
}

// NewJoinBundleIssuer wires the issuer. caFile falls back to a ca.crt sibling
// of tlsCertFile when unset, matching the hub-tls secret's three-key layout
// (ca.crt / server.crt / server.key). publicURL must be non-empty; an empty
// one yields an issuer whose Issue always fails with ErrJoinBundleUnavailable.
func NewJoinBundleIssuer(targets *TargetService, publicURL, caFile, tlsCertFile, runnerVersion string) *JoinBundleIssuer {
	if caFile == "" && tlsCertFile != "" {
		caFile = filepath.Join(filepath.Dir(tlsCertFile), "ca.crt")
	}
	return &JoinBundleIssuer{
		targets:       targets,
		publicURL:     publicURL,
		caFile:        caFile,
		runnerVersion: runnerVersion,
	}
}

// Issue assembles the bundle for target id: deployment facts first (config
// errors must surface before any DB touch, and are independently testable),
// then target identity and a fresh one-time enroll token (§9.9).
func (j *JoinBundleIssuer) Issue(id uuid.UUID) (*JoinBundle, error) {
	if j.publicURL == "" {
		return nil, ErrJoinBundleUnavailable
	}
	caPem, fingerprint, err := j.loadCA()
	if err != nil {
		return nil, err
	}
	tg, err := j.targets.Get(id)
	if err != nil {
		return nil, err
	}
	token, err := j.targets.GenerateEnrollToken(id)
	if err != nil {
		return nil, err
	}
	return &JoinBundle{
		TargetName:    tg.Name,
		GatewayURL:    j.publicURL + "/gateway/ws",
		APIBaseURL:    j.publicURL,
		CAPem:         caPem,
		CAFingerprint: fingerprint,
		EnrollToken:   token,
		ExpiresIn:     "24h",
		RunnerVersion: j.runnerVersion,
	}, nil
}

func (j *JoinBundleIssuer) loadCA() (string, string, error) {
	if j.caFile == "" {
		return "", "", ErrJoinBundleUnavailable
	}
	pemBytes, err := os.ReadFile(j.caFile)
	if err != nil {
		return "", "", fmt.Errorf("%w: read CA %s: %v", ErrJoinBundleUnavailable, j.caFile, err)
	}
	if !utf8.Valid(pemBytes) {
		return "", "", fmt.Errorf("%w: CA %s is not PEM", ErrJoinBundleUnavailable, j.caFile)
	}
	block, _ := pem.Decode(pemBytes)
	if block == nil || block.Type != "CERTIFICATE" {
		return "", "", fmt.Errorf("%w: CA %s has no CERTIFICATE block", ErrJoinBundleUnavailable, j.caFile)
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return "", "", fmt.Errorf("%w: parse CA %s: %v", ErrJoinBundleUnavailable, j.caFile, err)
	}
	sum := sha256.Sum256(cert.RawSubjectPublicKeyInfo)
	return string(pemBytes), "sha256:" + hex.EncodeToString(sum[:]), nil
}
