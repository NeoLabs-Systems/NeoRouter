package providers

// Provider keys at rest are sealed by internal/secrets; these names are the
// providers domain's view of it.

import "github.com/NeoLabs-Systems/neorouter/internal/secrets"

// SealedPrefix marks a value sealed with neolabs-go/secretbox.
const SealedPrefix = secrets.Prefix

// SecretKeySize is the size of the raw secret.key file.
const SecretKeySize = secrets.KeySize

var (
	// ErrMalformedSecret is returned for a stored value that cannot be decrypted.
	ErrMalformedSecret = secrets.ErrMalformed
	// ErrSecretKeySize is returned for a secret.key of the wrong size.
	ErrSecretKeySize = secrets.ErrKeySize
)

// Sealer opens stored provider secrets and seals new ones.
type Sealer = secrets.Sealer

var (
	// NewSealer seals with a box and opens legacy values with the legacy key.
	NewSealer = secrets.NewWithBox
	// NewSecretKeySealer builds the sealer from the secret.key file.
	NewSecretKeySealer = secrets.FromFile
	// NewSealerFromKey builds the sealer from the secret.key bytes.
	NewSealerFromKey = secrets.New
	// LoadSecretKey reads secret.key.
	LoadSecretKey = secrets.LoadKey
	// EnsureSecretKey reads secret.key, creating it when missing.
	EnsureSecretKey = secrets.EnsureKey
	// OpenLegacy decrypts a value in the legacy format of earlier releases.
	OpenLegacy = secrets.OpenLegacy
	// SealLegacy writes the legacy format.
	SealLegacy = secrets.SealLegacy
)
