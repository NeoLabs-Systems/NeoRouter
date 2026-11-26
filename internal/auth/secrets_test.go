package auth_test

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"strings"
	"testing"

	"github.com/NeoLabs-Systems/neorouter/internal/auth"
)

// TestSecretsRoundTripAndLegacy covers the value sealer: new values carry the
// new prefix, and the raw-key AES-256-GCM values the Node server wrote (which
// the manager's migration re-seals) are still readable.
func TestSecretsRoundTripAndLegacy(t *testing.T) {
	key := bytes.Repeat([]byte{0x2a}, 32)
	sealer, err := auth.NewSecrets(key)
	if err != nil {
		t.Fatalf("new sealer: %v", err)
	}
	const plain = "JBSWY3DPEHPK3PXPJBSWY3DPEHPK3PXP"

	sealed, err := sealer.Seal(plain)
	if err != nil {
		t.Fatalf("seal: %v", err)
	}
	if !strings.HasPrefix(sealed, auth.SecretPrefix) {
		t.Fatalf("sealed value should carry the prefix: %q", sealed)
	}
	opened, err := sealer.Open(sealed)
	if err != nil || opened != plain {
		t.Fatalf("open sealed: %q, %v", opened, err)
	}
	if again, err := sealer.Seal(sealed); err != nil || again != sealed {
		t.Fatalf("sealing is idempotent: %q, %v", again, err)
	}

	legacy := legacySeal(t, key, plain)
	if strings.HasPrefix(legacy, auth.SecretPrefix) {
		t.Fatalf("a legacy value has no prefix: %q", legacy)
	}
	migrated, err := sealer.Open(legacy)
	if err != nil || migrated != plain {
		t.Fatalf("open legacy: %q, %v", migrated, err)
	}

	if _, err := sealer.Open("not-a-sealed-value"); err == nil {
		t.Fatal("a malformed value must not open")
	}
	if _, err := auth.NewSecrets(nil); err == nil {
		t.Fatal("an empty key material must be refused")
	}
}

// TestSecretsOpenNodeValue checks the legacy path against a value the Node
// server's encryptString actually produced: base64url of iv(12) | tag(16) |
// ciphertext under 32 bytes of 0x2a, decrypting to JBSWY3DP…3PXP.
func TestSecretsOpenNodeValue(t *testing.T) {
	const nodeValue = "OkgtTJxn4aHnrKP7H7RSLwEFEeLl7LXmgukPeCvK2SslBmstJs1RB771QLrx4MRTXhgxlk6kwlimuM4M"
	sealer, err := auth.NewSecrets(bytes.Repeat([]byte{0x2a}, 32))
	if err != nil {
		t.Fatalf("new sealer: %v", err)
	}
	opened, err := sealer.Open(nodeValue)
	if err != nil {
		t.Fatalf("open a value written by the Node server: %v", err)
	}
	if opened != "JBSWY3DPEHPK3PXPJBSWY3DPEHPK3PXP" {
		t.Fatalf("opened %q", opened)
	}
}

// legacySeal writes a value the way server/utils/crypto.js encryptString did:
// base64url of iv(12) | tag(16) | ciphertext under the raw secret.key bytes.
func legacySeal(t *testing.T, key []byte, plain string) string {
	t.Helper()
	block, err := aes.NewCipher(key)
	if err != nil {
		t.Fatalf("cipher: %v", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		t.Fatalf("gcm: %v", err)
	}
	iv := make([]byte, 12)
	if _, err := rand.Read(iv); err != nil {
		t.Fatalf("iv: %v", err)
	}
	out := aead.Seal(nil, iv, []byte(plain), nil)
	cut := len(out) - 16
	payload := append(append(append([]byte{}, iv...), out[cut:]...), out[:cut]...)
	return base64.RawURLEncoding.EncodeToString(payload)
}
