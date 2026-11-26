package providers

// Provider keys at rest. account_service.js stores the account's secret with
// encryptString/decryptString (server/utils/crypto.js), which is NeoRouter's
// legacy scheme: raw-key AES-256-GCM, base64url, no prefix.
//
// Per the conversion plan, NEW values are sealed with neolabs-go/secretbox under
// the "enc:v1:" prefix, and legacy values stay readable (and re-sealable by the
// manager's migration step) through OpenLegacy. This is the same shape
// internal/proxies uses for proxy URLs.

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/NeoLabs-Systems/neolabs-go/secretbox"
)

// SealedPrefix marks a value sealed with neolabs-go/secretbox. It is the format
// the manager's re-seal step writes, so every domain must agree on it.
const SealedPrefix = "enc:v1:"

// SecretKeySize is the size of the raw secret.key file NeoRouter has always used.
const SecretKeySize = 32

var (
	// ErrMalformedSecret is crypto.js decryptString's 'Encrypted value is
	// malformed.'.
	ErrMalformedSecret = errors.New("Encrypted value is malformed.")
	// ErrSecretKeySize is crypto.js masterKey's text for a wrong-sized key file.
	ErrSecretKeySize = errors.New("NeoRouter secret.key must contain exactly 32 bytes.")
)

// Sealer opens stored provider secrets and seals new ones: secretbox for
// writes, the legacy raw-key scheme for values the JS server wrote.
type Sealer struct {
	box    *secretbox.Box
	legacy []byte
}

// NewSealer seals with box and opens legacy values with legacyKey, which may be
// nil when the caller only ever handles sealed values (tests, fresh installs).
func NewSealer(box *secretbox.Box, legacyKey []byte) *Sealer {
	return &Sealer{box: box, legacy: legacyKey}
}

// NewSecretKeySealer builds the production sealer from ~/.neorouter/secret.key:
// new values use secretbox keyed on hex(secret.key), the same material the
// migration re-seals with.
func NewSecretKeySealer(secretKeyPath string) (*Sealer, error) {
	key, err := EnsureSecretKey(secretKeyPath)
	if err != nil {
		return nil, err
	}
	box, err := secretbox.New(SealedPrefix, hex.EncodeToString(key))
	if err != nil {
		return nil, fmt.Errorf("providers: build sealer: %w", err)
	}
	return NewSealer(box, key), nil
}

// NewSealerFromKey builds a sealer from the install's secret.key material.
func NewSealerFromKey(key []byte) (*Sealer, error) {
	if len(key) != SecretKeySize {
		return nil, ErrSecretKeySize
	}
	box, err := secretbox.New(SealedPrefix, hex.EncodeToString(key))
	if err != nil {
		return nil, fmt.Errorf("providers: build sealer: %w", err)
	}
	return NewSealer(box, key), nil
}

// Seal encrypts a provider secret for storage.
func (s *Sealer) Seal(plain string) (string, error) {
	if s == nil || s.box == nil {
		return "", errors.New("providers: sealer is not configured")
	}
	sealed, err := s.box.Seal(plain)
	if err != nil {
		return "", fmt.Errorf("providers: seal: %w", err)
	}
	return sealed, nil
}

// Open decrypts a stored provider secret, accepting both the secretbox format
// and the legacy raw-key format so an un-migrated database keeps working.
func (s *Sealer) Open(value string) (string, error) {
	if s == nil {
		return "", errors.New("providers: sealer is not configured")
	}
	if s.box != nil && s.box.IsSealed(value) {
		plain, err := s.box.OpenSealed(value)
		if err != nil {
			return "", fmt.Errorf("providers: open secret: %w", err)
		}
		return plain, nil
	}
	if len(s.legacy) == 0 {
		return "", ErrMalformedSecret
	}
	return OpenLegacy(value, s.legacy)
}

// LoadSecretKey reads secret.key, which must hold exactly 32 raw bytes.
func LoadSecretKey(path string) ([]byte, error) {
	key, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("providers: read secret key: %w", err)
	}
	if len(key) != SecretKeySize {
		return nil, ErrSecretKeySize
	}
	return key, nil
}

// EnsureSecretKey reads secret.key, creating it with 0600 permissions and 32
// random bytes when it is absent, as crypto.js masterKey does.
func EnsureSecretKey(path string) ([]byte, error) {
	key, err := LoadSecretKey(path)
	if err == nil {
		return key, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	key = make([]byte, SecretKeySize)
	if _, err := rand.Read(key); err != nil {
		return nil, fmt.Errorf("providers: generate secret key: %w", err)
	}
	// O_EXCL keeps two first runs from overwriting each other's key.
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			return LoadSecretKey(path)
		}
		return nil, fmt.Errorf("providers: create secret key: %w", err)
	}
	defer file.Close()
	if _, err := file.Write(key); err != nil {
		return nil, fmt.Errorf("providers: write secret key: %w", err)
	}
	return key, nil
}

// OpenLegacy decrypts a NeoRouter legacy value: base64url of
// iv(12) | authTag(16) | ciphertext, AES-256-GCM with secret.key as the key.
// The manager's migration calls this to re-seal old rows with secretbox.
//
// Legacy compat: this is the only place the old scheme is still spoken.
func OpenLegacy(payload string, key []byte) (string, error) {
	if len(key) != SecretKeySize {
		return "", ErrSecretKeySize
	}
	raw, err := decodeBase64URL(payload)
	if err != nil || len(raw) < 12+16 {
		return "", ErrMalformedSecret
	}
	aead, err := newLegacyAEAD(key)
	if err != nil {
		return "", err
	}
	iv, tag, ciphertext := raw[:12], raw[12:28], raw[28:]
	joined := make([]byte, 0, len(ciphertext)+len(tag))
	joined = append(joined, ciphertext...)
	joined = append(joined, tag...)
	plain, err := aead.Open(nil, iv, joined, nil)
	if err != nil {
		return "", ErrMalformedSecret
	}
	return string(plain), nil
}

// SealLegacy encrypts plaintext in the legacy format. Only the migration and
// tests should need it: new writes go through Sealer.Seal.
func SealLegacy(plaintext string, key []byte) (string, error) {
	if len(key) != SecretKeySize {
		return "", ErrSecretKeySize
	}
	aead, err := newLegacyAEAD(key)
	if err != nil {
		return "", err
	}
	iv := make([]byte, 12)
	if _, err := rand.Read(iv); err != nil {
		return "", fmt.Errorf("providers: read random iv: %w", err)
	}
	out := aead.Seal(nil, iv, []byte(plaintext), nil)
	cut := len(out) - 16
	payload := make([]byte, 0, 12+len(out))
	payload = append(payload, iv...)
	payload = append(payload, out[cut:]...)
	payload = append(payload, out[:cut]...)
	return base64.RawURLEncoding.EncodeToString(payload), nil
}

func newLegacyAEAD(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("providers: legacy cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("providers: legacy gcm: %w", err)
	}
	return aead, nil
}

// decodeBase64URL accepts Node's unpadded base64url and tolerates padding.
func decodeBase64URL(value string) ([]byte, error) {
	value = strings.TrimRight(value, "=")
	return base64.RawURLEncoding.DecodeString(value)
}
