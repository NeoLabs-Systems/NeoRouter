package proxies

// The proxy credential formats: secretbox for new writes, the legacy raw-key
// AES-256-GCM values the JS server stored, and a database written by the JS
// server still working without a migration.

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/NeoLabs-Systems/neolabs-go/secretbox"
	"github.com/google/uuid"
)

func TestSealerSealsWithSecretboxAndOpensLegacy(t *testing.T) {
	legacyKey := bytes.Repeat([]byte{0x2a}, LegacyKeySize)
	box, err := secretbox.New(SealedPrefix, "sealer-test-key")
	if err != nil {
		t.Fatalf("box: %v", err)
	}
	sealer := NewSealer(box, legacyKey)

	const plain = "socks5://user:password@host.test:1080"
	sealed, err := sealer.Seal(plain)
	if err != nil {
		t.Fatalf("seal: %v", err)
	}
	if !strings.HasPrefix(sealed, SealedPrefix) {
		t.Fatalf("sealed value %q has no %q prefix", sealed, SealedPrefix)
	}
	if strings.Contains(sealed, "password") {
		t.Fatalf("sealed value leaks the plaintext: %q", sealed)
	}
	opened, err := sealer.Open(sealed)
	if err != nil || opened != plain {
		t.Fatalf("open sealed = %q, %v", opened, err)
	}

	legacy, err := SealLegacy(plain, legacyKey)
	if err != nil {
		t.Fatalf("seal legacy: %v", err)
	}
	if strings.HasPrefix(legacy, SealedPrefix) {
		t.Fatalf("legacy value must not carry the new prefix: %q", legacy)
	}
	if _, err := base64.RawURLEncoding.DecodeString(legacy); err != nil {
		t.Fatalf("legacy value is not unpadded base64url: %v", err)
	}
	opened, err = OpenLegacy(legacy, legacyKey)
	if err != nil || opened != plain {
		t.Fatalf("open legacy = %q, %v", opened, err)
	}
	// The same value opens through the sealer, so an un-migrated row keeps working.
	opened, err = sealer.Open(legacy)
	if err != nil || opened != plain {
		t.Fatalf("sealer open legacy = %q, %v", opened, err)
	}

	if _, err := OpenLegacy(legacy, bytes.Repeat([]byte{0x01}, LegacyKeySize)); !errors.Is(err, ErrMalformedSecret) {
		t.Fatalf("wrong key error = %v, want ErrMalformedSecret", err)
	}
	if _, err := OpenLegacy("not-base64!!", legacyKey); !errors.Is(err, ErrMalformedSecret) {
		t.Fatalf("malformed error = %v, want ErrMalformedSecret", err)
	}
	if _, err := OpenLegacy(legacy, []byte("short")); !errors.Is(err, ErrSecretKeySize) {
		t.Fatalf("short key error = %v, want ErrSecretKeySize", err)
	}
	if _, err := sealer.Open(legacy[:len(legacy)-4] + "AAAA"); !errors.Is(err, ErrMalformedSecret) {
		t.Fatalf("tampered error = %v, want ErrMalformedSecret", err)
	}
}

func TestSecretKeyFileHandling(t *testing.T) {
	path := filepath.Join(t.TempDir(), "secret.key")
	key, err := EnsureLegacyKey(path)
	if err != nil {
		t.Fatalf("ensure key: %v", err)
	}
	if len(key) != LegacyKeySize {
		t.Fatalf("generated key is %d bytes", len(key))
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat key: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("key permissions = %o, want 600", perm)
	}
	again, err := EnsureLegacyKey(path)
	if err != nil || !bytes.Equal(key, again) {
		t.Fatalf("ensure is not idempotent: %v", err)
	}

	short := filepath.Join(t.TempDir(), "short.key")
	if err := os.WriteFile(short, []byte("too short"), 0o600); err != nil {
		t.Fatalf("write short key: %v", err)
	}
	if _, err := LoadLegacyKey(short); !errors.Is(err, ErrSecretKeySize) {
		t.Fatalf("short key error = %v, want ErrSecretKeySize", err)
	}
	if _, err := LoadLegacyKey(filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Fatal("a missing key file should fail")
	}

	sealer, err := NewSecretKeySealer(t.TempDir() + "/secret.key")
	if err != nil {
		t.Fatalf("new sealer: %v", err)
	}
	sealed, err := sealer.Seal("socks5://h.test:1080")
	if err != nil {
		t.Fatalf("seal: %v", err)
	}
	if opened, err := sealer.Open(sealed); err != nil || opened != "socks5://h.test:1080" {
		t.Fatalf("open = %q, %v", opened, err)
	}
}

// TestLegacyStoredProxyStillWorks writes the row the way the JS server did and
// makes a request through it, which is the migration promise: no re-seal needed
// before traffic works.
func TestLegacyStoredProxyStillWorks(t *testing.T) {
	h := newHarness(t)
	socks := startSOCKS5(t)
	h.reset()

	legacyKey := h.sealer.legacy
	url := "socks5://127.0.0.1:" + strconv.Itoa(socks.port())
	sealed, err := SealLegacy(url, legacyKey)
	if err != nil {
		t.Fatalf("seal legacy: %v", err)
	}
	if _, err := h.db.W.Exec(
		`INSERT INTO outbound_proxies (id, fingerprint, endpoint, secret_encrypted) VALUES (?, ?, ?, ?)`,
		uuid.NewString(), fingerprint(url), "socks5://127.0.0.1:"+strconv.Itoa(socks.port()), sealed); err != nil {
		t.Fatalf("insert legacy row: %v", err)
	}

	resp, err := h.svc.Fetch(context.Background(), h.request(t, http.MethodGet, "http://"+h.originHost+"/", ""))
	if err != nil {
		t.Fatalf("fetch through a legacy-sealed proxy: %v", err)
	}
	h.closeBody(resp)
	if served := h.list()[0].Served; served != 1 {
		t.Fatalf("served = %d, want 1", served)
	}
}
