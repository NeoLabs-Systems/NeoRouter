// Package settings owns NeoRouter's two key/value stores and the account data a
// person can take with them or delete.
//
// A global setting is one JSON value per key and carries the switches the whole
// server reads: registration_open (auth), the Artificial Analysis key (catalog),
// the AI privacy filter (processing), the outbound proxy policy (proxies). A user
// setting is the same shape under (user_id, key); a user secret is a user setting
// whose string value is sealed with the install's secret key.
package settings

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"

	"github.com/NeoLabs-Systems/neolabs-go/dbx"
	"github.com/NeoLabs-Systems/neorouter/internal/config"
)

// KeyRegistrationOpen is the global setting auth reads to decide whether people
// may register.
const KeyRegistrationOpen = "registration_open"

// ErrNoSealer is returned by the secret accessors when no Sealer is wired.
var ErrNoSealer = errors.New("settings: no secret sealer is wired")

// Sealer seals and opens a user secret at rest. *secretbox.Box satisfies it; the
// app builds one from the install's key material with the "enc:v1:" prefix.
//
// Values written by earlier releases are legacy AES-256-GCM ciphertext without
// that prefix and are re-sealed at migration; Open returns an unsealed value
// unchanged, so a legacy value must not reach GetUserSecret.
type Sealer interface {
	Seal(value string) (string, error)
	Open(value string) (string, error)
}

// Service is the settings store, and the admin endpoints that read and write it.
//
// Routes registered by this service carry no authentication: mount them behind
// the admin guard (requireSession, resolveSpace, requireOwnSpace, requireAdmin).
type Service struct {
	DB     *dbx.DB
	Cfg    func() config.Config
	Log    *slog.Logger
	Sealer Sealer

	// MetadataStatus is catalog metadata_service.status(), part of GET and
	// PATCH /api/v1/admin/settings.
	MetadataStatus func() any
	// SetArtificialAnalysisKey stores or clears (nil) the catalog's key.
	SetArtificialAnalysisKey func(ctx context.Context, key *string) error
	// PrivacyModelStatus is processing privacy_model.status().
	PrivacyModelStatus func() any
	// SetPrivacyModelEnabled turns the AI privacy filter on or off.
	SetPrivacyModelEnabled func(ctx context.Context, enabled bool) error
}

func (s *Service) log() *slog.Logger {
	if s.Log != nil {
		return s.Log
	}
	return slog.Default()
}

// GetGlobal returns the stored JSON value of key and whether the key exists.
// A missing key is not an error: callers apply their own default.
func (s *Service) GetGlobal(ctx context.Context, key string) (json.RawMessage, bool, error) {
	var value string
	err := s.DB.R.QueryRowContext(ctx, `SELECT value FROM settings WHERE key = ?`, key).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("settings: read global %q: %w", key, err)
	}
	return json.RawMessage(value), true, nil
}

// SetGlobal stores value as JSON; nil stores null.
func (s *Service) SetGlobal(ctx context.Context, key string, value any) error {
	encoded, err := marshalNoEscape(value)
	if err != nil {
		return fmt.Errorf("settings: encode global %q: %w", key, err)
	}
	if err := s.SetGlobalRaw(ctx, key, encoded); err != nil {
		return err
	}
	return nil
}

// SetGlobalRaw stores an already-encoded JSON value.
func (s *Service) SetGlobalRaw(ctx context.Context, key string, value json.RawMessage) error {
	if _, err := s.DB.W.ExecContext(ctx, `INSERT INTO settings (key, value) VALUES (?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value`, key, string(value)); err != nil {
		return fmt.Errorf("settings: write global %q: %w", key, err)
	}
	return nil
}

// GetGlobalString decodes a global setting as text, returning def when the key
// is missing or holds something else.
func (s *Service) GetGlobalString(ctx context.Context, key, def string) string {
	raw, found, err := s.GetGlobal(ctx, key)
	if err != nil || !found {
		return def
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return def
	}
	return value
}

// GetGlobalBool decodes a global setting as a boolean, returning def when the
// key is missing or holds something else.
func (s *Service) GetGlobalBool(ctx context.Context, key string, def bool) bool {
	raw, found, err := s.GetGlobal(ctx, key)
	if err != nil || !found {
		return def
	}
	var value bool
	if err := json.Unmarshal(raw, &value); err != nil {
		return def
	}
	return value
}

// RegistrationOpen is registrationOpen(): the stored value must be the JSON
// literal true, so a string or a number does not open registration.
func (s *Service) RegistrationOpen(ctx context.Context) (bool, error) {
	raw, found, err := s.GetGlobal(ctx, KeyRegistrationOpen)
	if err != nil {
		return false, err
	}
	if !found {
		return false, nil
	}
	var open bool
	if err := json.Unmarshal(raw, &open); err != nil {
		return false, nil
	}
	return open, nil
}

// GetUserSetting returns the stored JSON value of one user's setting and
// whether it exists.
func (s *Service) GetUserSetting(ctx context.Context, userID, key string) (json.RawMessage, bool, error) {
	var value string
	err := s.DB.R.QueryRowContext(ctx, `SELECT value FROM user_settings WHERE user_id = ? AND key = ?`, userID, key).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("settings: read user setting %q: %w", key, err)
	}
	return json.RawMessage(value), true, nil
}

// SetUserSetting stores value as JSON for one user.
func (s *Service) SetUserSetting(ctx context.Context, userID, key string, value any) error {
	encoded, err := marshalNoEscape(value)
	if err != nil {
		return fmt.Errorf("settings: encode user setting %q: %w", key, err)
	}
	if err := s.SetUserSettingRaw(ctx, userID, key, encoded); err != nil {
		return err
	}
	return nil
}

// SetUserSettingRaw stores an already-encoded JSON value for one user.
func (s *Service) SetUserSettingRaw(ctx context.Context, userID, key string, value json.RawMessage) error {
	if _, err := s.DB.W.ExecContext(ctx, `INSERT INTO user_settings (user_id, key, value) VALUES (?, ?, ?)
		ON CONFLICT(user_id, key) DO UPDATE SET value = excluded.value`, userID, key, string(value)); err != nil {
		return fmt.Errorf("settings: write user setting %q: %w", key, err)
	}
	return nil
}

// DeleteUserSetting removes one user's setting; deleting a missing key is fine.
func (s *Service) DeleteUserSetting(ctx context.Context, userID, key string) error {
	if _, err := s.DB.W.ExecContext(ctx, `DELETE FROM user_settings WHERE user_id = ? AND key = ?`, userID, key); err != nil {
		return fmt.Errorf("settings: delete user setting %q: %w", key, err)
	}
	return nil
}

// GetUserJSON decodes one user's setting into dst and reports whether it
// existed, leaving dst alone when it did not.
func (s *Service) GetUserJSON(ctx context.Context, userID, key string, dst any) (bool, error) {
	raw, found, err := s.GetUserSetting(ctx, userID, key)
	if err != nil || !found {
		return false, err
	}
	if err := json.Unmarshal(raw, dst); err != nil {
		return false, fmt.Errorf("settings: decode user setting %q: %w", key, err)
	}
	return true, nil
}

// GetUserBool decodes one user's setting as a boolean, returning def when it is
// missing or holds something else.
func (s *Service) GetUserBool(ctx context.Context, userID, key string, def bool) bool {
	var value bool
	found, err := s.GetUserJSON(ctx, userID, key, &value)
	if err != nil || !found {
		return def
	}
	return value
}

// GetUserSecret opens one user secret. The second result reports whether a
// secret is stored; an empty stored value counts as none.
func (s *Service) GetUserSecret(ctx context.Context, userID, key string) (string, bool, error) {
	raw, found, err := s.GetUserSetting(ctx, userID, key)
	if err != nil || !found {
		return "", false, err
	}
	var sealed string
	if err := json.Unmarshal(raw, &sealed); err != nil {
		return "", false, fmt.Errorf("settings: decode user secret %q: %w", key, err)
	}
	if sealed == "" {
		return "", false, nil
	}
	if s.Sealer == nil {
		return "", false, ErrNoSealer
	}
	plain, err := s.Sealer.Open(sealed)
	if err != nil {
		return "", false, fmt.Errorf("settings: open user secret %q: %w", key, err)
	}
	return plain, true, nil
}

// SetUserSecret seals and stores one user secret; an empty secret deletes it.
func (s *Service) SetUserSecret(ctx context.Context, userID, key, secret string) error {
	if secret == "" {
		return s.DeleteUserSetting(ctx, userID, key)
	}
	if s.Sealer == nil {
		return ErrNoSealer
	}
	sealed, err := s.Sealer.Seal(secret)
	if err != nil {
		return fmt.Errorf("settings: seal user secret %q: %w", key, err)
	}
	encoded, err := marshalNoEscape(sealed)
	if err != nil {
		return fmt.Errorf("settings: encode user secret %q: %w", key, err)
	}
	return s.SetUserSettingRaw(ctx, userID, key, encoded)
}
