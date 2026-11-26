package auth

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/NeoLabs-Systems/neolabs-go/httpx"
	"github.com/NeoLabs-Systems/neolabs-go/webauthnx"
	"github.com/NeoLabs-Systems/neorouter/internal/httpserver"
	"github.com/google/uuid"
)

// Passkeys and security keys run on neolabs-go/webauthnx; this file is
// NeoRouter's storage for it. A challenge is kept server side because clients
// hold bearer tokens, not a cookie session.

const (
	purposeRegistration   = "registration"
	purposeAuthentication = "authentication"
)

func (s *Service) webauthn() *webauthnx.Service {
	c := s.cfg()
	return &webauthnx.Service{
		RPName:     "NeoRouter",
		Origins:    httpx.ParseOrigins(s.AllowedOrigins + "," + strings.TrimRight(c.PublicURL, "/")),
		TrustProxy: c.TrustProxy,
		Store:      credentialStore{s},
		Accounts:   passkeyAccounts{s},
		Now:        s.now,
	}
}

func ceremonyError(err error) error {
	var failure *webauthnx.Error
	if errors.As(err, &failure) {
		return httpserver.New(failure.Status, "SECURITY_KEY_ERROR", failure.Message)
	}
	return err
}

type passkeyAccounts struct{ s *Service }

func passkeyIdentity(user *userRow, err error) (webauthnx.Identity, bool, error) {
	if err != nil || user == nil {
		return webauthnx.Identity{}, false, err
	}
	return webauthnx.Identity{ID: user.ID, Username: user.Username, DisplayName: user.DisplayName.String, Email: user.Email}, true, nil
}

func (a passkeyAccounts) ByID(ctx context.Context, userID string) (webauthnx.Identity, bool, error) {
	return passkeyIdentity(a.s.findUser(ctx, a.s.DB.R, userID))
}

func (a passkeyAccounts) ForSignIn(ctx context.Context, identifier string) (webauthnx.Identity, bool, error) {
	return passkeyIdentity(a.s.userByAccount(ctx, identifier))
}

type credentialStore struct{ s *Service }

func (c credentialStore) query(ctx context.Context, where string, args ...any) ([]webauthnx.Credential, error) {
	rows, err := c.s.DB.R.QueryContext(ctx, `SELECT id, user_id, credential_id, public_key, counter, rp_id, transports,
		COALESCE(device_type, ''), backed_up, user_verified, label, created_at, COALESCE(last_used_at, '')
		FROM user_webauthn_credentials WHERE `+where, args...)
	if err != nil {
		return nil, fmt.Errorf("auth: read security keys: %w", err)
	}
	defer rows.Close()
	var out []webauthnx.Credential
	for rows.Next() {
		var cred webauthnx.Credential
		var transports string
		if err := rows.Scan(&cred.RowID, &cred.UserID, &cred.CredentialID, &cred.PublicKey, &cred.Counter, &cred.RPID,
			&transports, &cred.DeviceType, &cred.BackedUp, &cred.UserVerified, &cred.Label, &cred.CreatedAt, &cred.LastUsedAt); err != nil {
			return nil, fmt.Errorf("auth: read security key: %w", err)
		}
		if err := json.Unmarshal([]byte(transports), &cred.Transports); err != nil {
			return nil, fmt.Errorf("auth: read security key transports: %w", err)
		}
		out = append(out, cred)
	}
	return out, rows.Err()
}

func (c credentialStore) ListForUser(ctx context.Context, userID string) ([]webauthnx.Credential, error) {
	return c.query(ctx, `user_id = ? ORDER BY id DESC`, userID)
}

func (c credentialStore) ForUserAndRP(ctx context.Context, userID, rpID string) ([]webauthnx.Credential, error) {
	return c.query(ctx, `user_id = ? AND rp_id = ?`, userID, rpID)
}

func (c credentialStore) CountForRP(ctx context.Context, rpID string) (int, error) {
	var count int
	err := c.s.DB.R.QueryRowContext(ctx, `SELECT COUNT(*) FROM user_webauthn_credentials WHERE rp_id = ?`, rpID).Scan(&count)
	return count, err
}

func (c credentialStore) FindByCredentialID(ctx context.Context, credentialID, rpID string) (*webauthnx.Credential, error) {
	found, err := c.query(ctx, `credential_id = ? AND rp_id = ?`, credentialID, rpID)
	if err != nil || len(found) == 0 {
		return nil, err
	}
	return &found[0], nil
}

func (c credentialStore) Exists(ctx context.Context, credentialID string) (bool, error) {
	found, err := c.query(ctx, `credential_id = ?`, credentialID)
	return len(found) > 0, err
}

func (c credentialStore) Insert(ctx context.Context, cred webauthnx.Credential) error {
	transports, err := json.Marshal(cred.Transports)
	if err != nil {
		return err
	}
	_, err = c.s.DB.W.ExecContext(ctx, `INSERT INTO user_webauthn_credentials
		(user_id, credential_id, public_key, counter, rp_id, transports, device_type, backed_up, user_verified, label)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		cred.UserID, cred.CredentialID, cred.PublicKey, cred.Counter, cred.RPID, string(transports),
		cred.DeviceType, cred.BackedUp, cred.UserVerified, cred.Label)
	return err
}

func (c credentialStore) UpdateAfterLogin(ctx context.Context, rowID int64, counter uint32, userVerified bool) error {
	_, err := c.s.DB.W.ExecContext(ctx, `UPDATE user_webauthn_credentials SET counter = ?, user_verified = ?,
		last_used_at = strftime('%Y-%m-%dT%H:%M:%fZ','now') WHERE id = ?`, counter, userVerified, rowID)
	return err
}

func (c credentialStore) changed(ctx context.Context, query string, args ...any) (bool, error) {
	result, err := c.s.DB.W.ExecContext(ctx, query, args...)
	if err != nil {
		return false, err
	}
	count, err := result.RowsAffected()
	return count > 0, err
}

func (c credentialStore) Rename(ctx context.Context, userID string, rowID int64, label string) (bool, error) {
	return c.changed(ctx, `UPDATE user_webauthn_credentials SET label = ? WHERE id = ? AND user_id = ?`, label, rowID, userID)
}

func (c credentialStore) Delete(ctx context.Context, userID string, rowID int64) (bool, error) {
	return c.changed(ctx, `DELETE FROM user_webauthn_credentials WHERE id = ? AND user_id = ?`, rowID, userID)
}

func (s *Service) savePending(ctx context.Context, purpose, userID string, pending *webauthnx.Pending) (string, error) {
	raw, err := json.Marshal(pending)
	if err != nil {
		return "", err
	}
	if _, err := s.DB.W.ExecContext(ctx, `DELETE FROM webauthn_challenges WHERE expires_at <= ?`, isoMillis(s.now())); err != nil {
		return "", fmt.Errorf("auth: prune challenges: %w", err)
	}
	id := uuid.NewString()
	var owner sql.NullString
	if userID != "" {
		owner = sql.NullString{String: userID, Valid: true}
	}
	if _, err := s.DB.W.ExecContext(ctx,
		`INSERT INTO webauthn_challenges (id, purpose, user_id, challenge, rp_id, expires_at) VALUES (?, ?, ?, ?, ?, ?)`,
		id, purpose, owner, string(raw), pending.RPID, isoMillis(time.UnixMilli(pending.ExpiresAt))); err != nil {
		return "", fmt.Errorf("auth: store challenge: %w", err)
	}
	return id, nil
}

// takePending spends a challenge: it can be answered once.
func (s *Service) takePending(ctx context.Context, id, purpose, userID string) (*webauthnx.Pending, error) {
	expired := httpserver.New(http.StatusBadRequest, "CHALLENGE_EXPIRED", "The security key request expired. Please try again.")
	var raw string
	err := s.DB.W.QueryRowContext(ctx, `DELETE FROM webauthn_challenges
		WHERE id = ? AND purpose = ? AND COALESCE(user_id, '') = ? RETURNING challenge`,
		strings.TrimSpace(id), purpose, userID).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, expired
	}
	if err != nil {
		return nil, fmt.Errorf("auth: read challenge: %w", err)
	}
	var pending webauthnx.Pending
	if err := json.Unmarshal([]byte(raw), &pending); err != nil {
		return nil, expired
	}
	return &pending, nil
}

// passkeyOptions starts adding a key, which needs the password again: a stolen
// session must not be able to plant a lasting way in.
func (s *Service) passkeyOptions(ctx context.Context, r *http.Request, userID, currentPassword string) (string, any, error) {
	if err := s.assertPassword(ctx, userID, currentPassword); err != nil {
		return "", nil, err
	}
	options, pending, err := s.webauthn().BeginRegistration(ctx, r, userID)
	if err != nil {
		return "", nil, ceremonyError(err)
	}
	id, err := s.savePending(ctx, purposeRegistration, userID, pending)
	return id, options, err
}

func (s *Service) addPasskey(ctx context.Context, r *http.Request, userID, challengeID string, response json.RawMessage, label string) ([]webauthnx.Payload, error) {
	pending, err := s.takePending(ctx, challengeID, purposeRegistration, userID)
	if err != nil {
		return nil, err
	}
	if err := s.webauthn().CompleteRegistration(ctx, r, userID, response, label, pending); err != nil {
		return nil, ceremonyError(err)
	}
	return s.webauthn().List(ctx, userID)
}

func (s *Service) passkeySignInOptions(ctx context.Context, r *http.Request, account string) (string, any, error) {
	options, pending, err := s.webauthn().BeginLogin(ctx, r, account)
	if err != nil {
		return "", nil, ceremonyError(err)
	}
	id, err := s.savePending(ctx, purposeAuthentication, "", pending)
	return id, options, err
}

// passkeySignIn verifies an assertion. A key that checked a PIN or biometric
// already counts as two factors; any other asks for the code when 2FA is on.
func (s *Service) passkeySignIn(ctx context.Context, r *http.Request, challengeID string, response json.RawMessage, code string, rc RequestContext) (LoginResult, error) {
	pending, err := s.takePending(ctx, challengeID, purposeAuthentication, "")
	if err != nil {
		return LoginResult{}, err
	}
	verified, err := s.webauthn().CompleteLogin(ctx, r, response, pending)
	if err != nil {
		return LoginResult{}, ceremonyError(err)
	}
	if !verified.UserVerified {
		if err := s.requireCode(ctx, verified.UserID, code); err != nil {
			return LoginResult{}, err
		}
	}
	return s.signIn(ctx, verified.UserID, rc)
}

// requireCode is the sign-in code step in one request.
func (s *Service) requireCode(ctx context.Context, userID, code string) error {
	enabled, err := s.twoFactorEnabled(ctx, userID)
	if err != nil || !enabled {
		return err
	}
	if strings.TrimSpace(code) == "" {
		return httpserver.New(http.StatusUnauthorized, "TWO_FACTOR_REQUIRED", "A two-factor authentication code is required.")
	}
	valid, err := s.verifyCode(ctx, userID, code)
	if err != nil {
		return err
	}
	if !valid {
		return httpserver.New(http.StatusUnauthorized, "INVALID_TWO_FACTOR", "The code is not valid.")
	}
	return nil
}
