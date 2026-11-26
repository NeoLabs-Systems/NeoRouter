package secrets_test

import (
	"bytes"
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/NeoLabs-Systems/neorouter/internal/secrets"
)

func TestSealerSealsAndOpensBothFormats(t *testing.T) {
	key := bytes.Repeat([]byte{0x2a}, secrets.KeySize)
	sealer, err := secrets.New(key)
	if err != nil {
		t.Fatal(err)
	}
	const plain = "socks5://user:password@host.test:1080"

	sealed, err := sealer.Seal(plain)
	if err != nil || !strings.HasPrefix(sealed, secrets.Prefix) || strings.Contains(sealed, "password") {
		t.Fatalf("seal = %q, %v", sealed, err)
	}
	if again, err := sealer.Seal(sealed); err != nil || again != sealed {
		t.Fatalf("sealing is idempotent: %q, %v", again, err)
	}
	if opened, err := sealer.Open(sealed); err != nil || opened != plain {
		t.Fatalf("open sealed = %q, %v", opened, err)
	}

	legacy, err := secrets.SealLegacy(plain, key)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := base64.RawURLEncoding.DecodeString(legacy); err != nil {
		t.Fatalf("legacy value is not unpadded base64url: %v", err)
	}
	if opened, err := sealer.Open(legacy); err != nil || opened != plain {
		t.Fatalf("open legacy = %q, %v", opened, err)
	}

	if _, err := secrets.OpenLegacy(legacy, bytes.Repeat([]byte{1}, secrets.KeySize)); !errors.Is(err, secrets.ErrMalformed) {
		t.Fatalf("wrong key error = %v", err)
	}
	for _, bad := range []string{"", "not-base64!!", legacy[:len(legacy)-4] + "AAAA"} {
		if _, err := sealer.Open(bad); !errors.Is(err, secrets.ErrMalformed) {
			t.Fatalf("open %q error = %v", bad, err)
		}
	}
	if _, err := secrets.New([]byte("short")); !errors.Is(err, secrets.ErrKeySize) {
		t.Fatalf("short key error = %v", err)
	}
}

// A value in the legacy format, sealed under 32 bytes of 0x2a.
func TestOpenNodeValue(t *testing.T) {
	const nodeValue = "OkgtTJxn4aHnrKP7H7RSLwEFEeLl7LXmgukPeCvK2SslBmstJs1RB771QLrx4MRTXhgxlk6kwlimuM4M"
	sealer, err := secrets.New(bytes.Repeat([]byte{0x2a}, secrets.KeySize))
	if err != nil {
		t.Fatal(err)
	}
	if opened, err := sealer.Open(nodeValue); err != nil || opened != "JBSWY3DPEHPK3PXPJBSWY3DPEHPK3PXP" {
		t.Fatalf("opened %q, %v", opened, err)
	}
}

func TestEnsureKeyFirstRunsAgree(t *testing.T) {
	path := filepath.Join(t.TempDir(), "data", "secret.key")
	keys := make([][]byte, 16)
	var wait sync.WaitGroup
	for i := range keys {
		wait.Add(1)
		go func() {
			defer wait.Done()
			key, err := secrets.EnsureKey(path)
			if err != nil {
				t.Error(err)
			}
			keys[i] = key
		}()
	}
	wait.Wait()
	for _, key := range keys {
		if len(key) != secrets.KeySize || !bytes.Equal(key, keys[0]) {
			t.Fatal("concurrent first runs produced different keys")
		}
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("key file mode %v, %v", info.Mode(), err)
	}
	if entries, _ := os.ReadDir(filepath.Dir(path)); len(entries) != 1 {
		t.Fatalf("temporary files left behind: %v", entries)
	}

	short := filepath.Join(t.TempDir(), "short.key")
	if err := os.WriteFile(short, []byte("too short"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := secrets.EnsureKey(short); !errors.Is(err, secrets.ErrKeySize) {
		t.Fatalf("a wrong-sized key must be refused, not replaced: %v", err)
	}
}
