package codec

import (
	"os"
	"strings"
	"sync"
	"testing"
)

func TestEncryptDecryptRoundTrip(t *testing.T) {
	os.Setenv("CREDENTIAL_ENCRYPTION_KEY", "0123456789abcdef0123456789abcdef") // 32 bytes
	defer os.Unsetenv("CREDENTIAL_ENCRYPTION_KEY")
	// reset cached key so the env change takes effect
	key = nil
	keyOnce = sync.Once{}

	plain := "kubeconfig-secret-value-12345"
	c, err := Encrypt(plain)
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	if !strings.HasPrefix(c, prefix) {
		t.Fatalf("expected ciphertext prefix, got %q", c)
	}
	if c == plain {
		t.Fatalf("ciphertext must not equal plaintext")
	}
	got, err := Decrypt(c)
	if err != nil {
		t.Fatalf("Decrypt: %v", err)
	}
	if got != plain {
		t.Fatalf("round-trip mismatch: got %q want %q", got, plain)
	}
}

func TestNoKeyStoresPlaintext(t *testing.T) {
	os.Unsetenv("CREDENTIAL_ENCRYPTION_KEY")
	key = nil
	keyOnce = sync.Once{}

	c, err := Encrypt("hello")
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	if c != "hello" {
		t.Fatalf("without a key, value should be stored plaintext, got %q", c)
	}
	got, err := Decrypt(c)
	if err != nil {
		t.Fatalf("Decrypt: %v", err)
	}
	if got != "hello" {
		t.Fatalf("decrypt of plaintext should return as-is, got %q", got)
	}
}

func TestDecryptLegacyPlaintextIsNoOp(t *testing.T) {
	// A value stored before encryption (no prefix) must survive decrypt.
	got, err := Decrypt("old-plaintext-secret")
	if err != nil {
		t.Fatalf("Decrypt: %v", err)
	}
	if got != "old-plaintext-secret" {
		t.Fatalf("legacy plaintext must pass through, got %q", got)
	}
}

func TestDifferentNonceEachCall(t *testing.T) {
	os.Setenv("CREDENTIAL_ENCRYPTION_KEY", "0123456789abcdef0123456789abcdef")
	defer os.Unsetenv("CREDENTIAL_ENCRYPTION_KEY")
	key = nil
	keyOnce = sync.Once{}

	a, _ := Encrypt("same")
	b, _ := Encrypt("same")
	if a == b {
		t.Fatalf("two encryptions of the same plaintext must differ (random nonce)")
	}
}

func TestDecryptWrongKeyFails(t *testing.T) {
	os.Setenv("CREDENTIAL_ENCRYPTION_KEY", "0123456789abcdef0123456789abcdef")
	key = nil
	keyOnce = sync.Once{}
	c, _ := Encrypt("secret")
	// now "lose" the key and supply a different one
	os.Setenv("CREDENTIAL_ENCRYPTION_KEY", "abcdef0123456789abcdef0123456789")
	key = nil
	keyOnce = sync.Once{}
	if _, err := Decrypt(c); err == nil {
		t.Fatalf("decrypt with wrong key must fail")
	}
}
