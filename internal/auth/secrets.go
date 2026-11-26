package auth

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/NeoLabs-Systems/neolabs-go/secretbox"
)

// SecretPrefix marks a value sealed with the Go server's format. Values without
// it were written by the Node server (raw-key AES-256-GCM, base64url) and are
// re-sealed by the manager's migration; Open still reads them so a database that
// has not been re-sealed yet keeps working.
const SecretPrefix = "enc:v1:"

// Secrets seals and opens the values NeoRouter stores encrypted at rest.
type Secrets interface {
	Seal(value string) (string, error)
	Open(value string) (string, error)
}

// NewSecrets builds the sealer of one install from its secret.key material,
// exactly as the manager's re-seal step does:
// secretbox.New("enc:v1:", hex(secretKey)).
func NewSecrets(key []byte) (Secrets, error) {
	box, err := secretbox.New(SecretPrefix, hex.EncodeToString(key))
	if err != nil {
		return nil, err
	}
	return &sealed{box: box, key: append([]byte(nil), key...)}, nil
}

type sealed struct {
	box *secretbox.Box
	key []byte
}

func (s *sealed) Seal(value string) (string, error) { return s.box.Seal(value) }

func (s *sealed) Open(value string) (string, error) {
	if value == "" || s.box.IsSealed(value) {
		return s.box.Open(value)
	}
	return legacyOpen(s.key, value)
}

// legacyOpen decrypts a value the Node server wrote with
// server/utils/crypto.js decryptString: base64url of iv(12) | tag(16) |
// ciphertext, AES-256-GCM under the raw secret.key bytes.
func legacyOpen(key []byte, payload string) (string, error) {
	raw, err := base64.RawURLEncoding.DecodeString(payload)
	if err != nil {
		// The Node decoder tolerated padding.
		raw, err = base64.URLEncoding.DecodeString(payload)
		if err != nil {
			return "", fmt.Errorf("auth: decode legacy secret: %w", err)
		}
	}
	if len(raw) < 12+16 {
		return "", errors.New("auth: encrypted value is malformed")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", fmt.Errorf("auth: legacy secret key: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("auth: legacy secret cipher: %w", err)
	}
	sealed := make([]byte, 0, len(raw)-12)
	sealed = append(sealed, raw[28:]...)
	sealed = append(sealed, raw[12:28]...)
	plain, err := aead.Open(nil, raw[:12], sealed, nil)
	if err != nil {
		return "", fmt.Errorf("auth: decrypt legacy secret: %w", err)
	}
	return string(plain), nil
}

// readSecretKey reads the install's 32-byte secret.key, creating it on first
// use as runtime/paths.js does, so a fresh install has somewhere to seal to.
func readSecretKey(path string) ([]byte, error) {
	key, err := os.ReadFile(path)
	if err == nil {
		if len(key) != 32 {
			return nil, errors.New("NeoRouter secret.key must contain exactly 32 bytes.")
		}
		return key, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("auth: read secret key: %w", err)
	}
	fresh := make([]byte, 32)
	if _, err := rand.Read(fresh); err != nil {
		return nil, fmt.Errorf("auth: generate secret key: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("auth: create key directory: %w", err)
	}
	// O_EXCL keeps two first runs from overwriting each other's key.
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			return readSecretKey(path)
		}
		return nil, fmt.Errorf("auth: create secret key: %w", err)
	}
	defer file.Close()
	if _, err := file.Write(fresh); err != nil {
		return nil, fmt.Errorf("auth: write secret key: %w", err)
	}
	return fresh, nil
}
