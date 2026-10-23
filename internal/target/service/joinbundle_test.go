package service

// JoinBundleIssuer 单测（ADR-0002）：配置缺失显式 501 语义（不触 DB）、
// ca.crt 兄弟文件推导、RFC7469 SPKI SHA256 指纹（kubeadm 同款格式）。
// TargetService 依赖具体 GORM repo，完整 happy path 由真集群 E2E 覆盖，
// 这里只测 Issue 的配置守卫段（顺序保证：配置错误先于任何 DB 访问）。

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestIssueUnavailableWithoutPublicURL(t *testing.T) {
	issuer := NewJoinBundleIssuer(nil, "", "", "", "v0.0.1")
	_, err := issuer.Issue(uuid.New())
	if !errors.Is(err, ErrJoinBundleUnavailable) {
		t.Fatalf("want ErrJoinBundleUnavailable, got %v", err)
	}
}

func TestIssueUnavailableWhenCAFileMissing(t *testing.T) {
	issuer := NewJoinBundleIssuer(nil, "https://hub.example.com:8443",
		filepath.Join(t.TempDir(), "nope.crt"), "", "v0.0.1")
	_, err := issuer.Issue(uuid.New())
	if !errors.Is(err, ErrJoinBundleUnavailable) {
		t.Fatalf("want ErrJoinBundleUnavailable, got %v", err)
	}
}

func TestDerivedCAFileIsSiblingOfCert(t *testing.T) {
	issuer := NewJoinBundleIssuer(nil, "https://hub.example.com:8443",
		"", "/etc/hub-tls/server.crt", "v0.0.1")
	if issuer.caFile != "/etc/hub-tls/ca.crt" {
		t.Fatalf("caFile fallback = %q, want /etc/hub-tls/ca.crt", issuer.caFile)
	}
}

func TestLoadCAFingerprintMatchesSPKISHA256(t *testing.T) {
	dir := t.TempDir()
	caPath := filepath.Join(dir, "ca.crt")
	writeTestCA(t, caPath)

	issuer := NewJoinBundleIssuer(nil, "https://hub.example.com:8443", caPath, "", "v0.0.1")
	caPem, fingerprint, err := issuer.loadCA()
	if err != nil {
		t.Fatalf("loadCA: %v", err)
	}
	if !strings.HasPrefix(caPem, "-----BEGIN CERTIFICATE-----") {
		t.Fatalf("caPem is not PEM: %q", caPem[:20])
	}
	// kubeadm 同款：sha256:<hex of DER-encoded SubjectPublicKeyInfo>。
	pemBytes, _ := os.ReadFile(caPath)
	block, _ := pem.Decode(pemBytes)
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatalf("parse test CA: %v", err)
	}
	sum := sha256.Sum256(cert.RawSubjectPublicKeyInfo)
	if fingerprint != "sha256:"+hex.EncodeToString(sum[:]) {
		t.Fatalf("fingerprint = %q, want sha256:%s", fingerprint, hex.EncodeToString(sum[:]))
	}
}

func TestLoadCARejectsNonPEM(t *testing.T) {
	dir := t.TempDir()
	caPath := filepath.Join(dir, "ca.crt")
	if err := os.WriteFile(caPath, []byte("definitely not pem"), 0o600); err != nil {
		t.Fatal(err)
	}
	issuer := NewJoinBundleIssuer(nil, "https://hub.example.com:8443", caPath, "", "v0.0.1")
	_, _, err := issuer.loadCA()
	if !errors.Is(err, ErrJoinBundleUnavailable) {
		t.Fatalf("want ErrJoinBundleUnavailable, got %v", err)
	}
}

// --- helpers ---

func writeTestCA(t *testing.T, path string) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	tpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "sdp-test-ca"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		IsCA:         true,
		KeyUsage:     x509.KeyUsageCertSign,
	}
	der, err := x509.CreateCertificate(rand.Reader, tpl, tpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	pemBytes := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	if err := os.WriteFile(path, pemBytes, 0o600); err != nil {
		t.Fatal(err)
	}
}
