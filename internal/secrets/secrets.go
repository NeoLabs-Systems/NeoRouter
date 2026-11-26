// Package secrets seals the values NeoRouter stores encrypted (provider keys,
// proxy URLs, second-factor secrets, settings) and owns the secret.key file
// they are sealed with.
//
// New values are neolabs-go/secretbox "enc:v1:" values keyed on
// hex(secret.key). Values written by earlier releases (base64url of iv | tag |
// ciphertext, AES-256-GCM under the raw key bytes) stay readable.
package secrets

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/NeoLabs-Systems/neolabs-go/secretbox"
)

const (
	// Prefix marks a value sealed with secretbox.
	Prefix = "enc:v1:"
	// KeySize is the size of the raw secret.key file.
	KeySize = 32

	ivSize  = 12
	tagSize = 16
)

var (
	// ErrMalformed is returned for a stored value that cannot be decrypted.
	ErrMalformed = errors.New("Encrypted value is malformed.")
	// ErrKeySize is returned for a secret.key of the wrong size.
	ErrKeySize = errors.New("NeoRouter secret.key must contain exactly 32 bytes.")
)

// Sealer seals new values and opens both formats.
type Sealer struct {
	box    *secretbox.Box
	legacy []byte
}

// New builds the sealer of the install whose secret.key holds key.
func New(key []byte) (*Sealer, error) {
	if len(key) != KeySize {
		return nil, ErrKeySize
	}
	box, err := secretbox.New(Prefix, hex.EncodeToString(key))
	if err != nil {
		return nil, fmt.Errorf("secrets: build sealer: %w", err)
	}
	return NewWithBox(box, append([]byte(nil), key...)), nil
}

// NewWithBox seals with box and opens legacy values with legacyKey, which may
// be nil when only sealed values are expected.
func NewWithBox(box *secretbox.Box, legacyKey []byte) *Sealer {
	return &Sealer{box: box, legacy: legacyKey}
}

// FromFile is New with the key read from path, creating it when missing.
func FromFile(path string) (*Sealer, error) {
	key, err := EnsureKey(path)
	if err != nil {
		return nil, err
	}
	return New(key)
}

// Seal encrypts a value for storage.
func (s *Sealer) Seal(plain string) (string, error) {
	if s == nil || s.box == nil {
		return "", errors.New("secrets: sealer is not configured")
	}
	sealed, err := s.box.Seal(plain)
	if err != nil {
		return "", fmt.Errorf("secrets: seal: %w", err)
	}
	return sealed, nil
}

// Open decrypts a stored value.
func (s *Sealer) Open(value string) (string, error) {
	if s == nil {
		return "", errors.New("secrets: sealer is not configured")
	}
	if s.box != nil && s.box.IsSealed(value) {
		plain, err := s.box.OpenSealed(value)
		if err != nil {
			return "", fmt.Errorf("secrets: open: %w", err)
		}
		return plain, nil
	}
	if len(s.legacy) == 0 {
		return "", ErrMalformed
	}
	return OpenLegacy(value, s.legacy)
}

// LoadKey reads secret.key.
func LoadKey(path string) ([]byte, error) {
	key, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("secrets: read secret key: %w", err)
	}
	if len(key) != KeySize {
		return nil, ErrKeySize
	}
	return key, nil
}

// EnsureKey reads secret.key, creating it with 32 random bytes (0600) when it
// is missing. The key is written to a private temporary file first and then
// linked into place, which fails if the file already exists: concurrent first
// runs agree on one key and nobody can read a half-written one.
func EnsureKey(path string) ([]byte, error) {
	key, err := LoadKey(path)
	if err == nil || !errors.Is(err, fs.ErrNotExist) {
		return key, err
	}
	key = make([]byte, KeySize)
	if _, err := rand.Read(key); err != nil {
		return nil, fmt.Errorf("secrets: generate secret key: %w", err)
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("secrets: create key directory: %w", err)
	}
	temp, err := os.CreateTemp(dir, ".secret-key-*") // O_EXCL, 0600
	if err != nil {
		return nil, fmt.Errorf("secrets: create secret key: %w", err)
	}
	defer os.Remove(temp.Name())
	_, err = temp.Write(key)
	if err = errors.Join(err, temp.Sync(), temp.Close()); err != nil {
		return nil, fmt.Errorf("secrets: write secret key: %w", err)
	}
	err = os.Link(temp.Name(), path)
	switch {
	case err == nil:
		return key, nil
	case errors.Is(err, fs.ErrExist):
		return LoadKey(path)
	}
	// A filesystem without hard links: create the file itself exclusively.
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if errors.Is(err, fs.ErrExist) {
		return LoadKey(path)
	}
	if err != nil {
		return nil, fmt.Errorf("secrets: create secret key: %w", err)
	}
	_, err = file.Write(key)
	if err = errors.Join(err, file.Close()); err != nil {
		return nil, fmt.Errorf("secrets: write secret key: %w", err)
	}
	return key, nil
}

// OpenLegacy decrypts a value in that legacy format.
func OpenLegacy(payload string, key []byte) (string, error) {
	aead, err := legacyAEAD(key)
	if err != nil {
		return "", err
	}
	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(payload, "="))
	if err != nil || len(raw) < ivSize+tagSize {
		return "", ErrMalformed
	}
	joined := append(append([]byte{}, raw[ivSize+tagSize:]...), raw[ivSize:ivSize+tagSize]...)
	plain, err := aead.Open(nil, raw[:ivSize], joined, nil)
	if err != nil {
		return "", ErrMalformed
	}
	return string(plain), nil
}

// SealLegacy writes the legacy format. Only migration tests need it.
func SealLegacy(plain string, key []byte) (string, error) {
	aead, err := legacyAEAD(key)
	if err != nil {
		return "", err
	}
	iv := make([]byte, ivSize)
	if _, err := rand.Read(iv); err != nil {
		return "", fmt.Errorf("secrets: read random iv: %w", err)
	}
	out := aead.Seal(nil, iv, []byte(plain), nil)
	cut := len(out) - tagSize
	payload := append(append(append([]byte{}, iv...), out[cut:]...), out[:cut]...)
	return base64.RawURLEncoding.EncodeToString(payload), nil
}

func legacyAEAD(key []byte) (cipher.AEAD, error) {
	if len(key) != KeySize {
		return nil, ErrKeySize
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("secrets: cipher: %w", err)
	}
	return cipher.NewGCM(block)
}
