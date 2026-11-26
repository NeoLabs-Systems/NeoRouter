package auth

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/NeoLabs-Systems/neolabs-go/passhash"
	"github.com/google/uuid"
)

// defaultSessionTTL is NEOROUTER_SESSION_TTL_MS' fallback, used when no
// configuration was supplied.
const defaultSessionTTL = 30 * 24 * time.Hour

// createSession issues an nrs_<base64url 32 bytes> session and stores only its
// SHA-256 digest.
func (s *Service) createSession(ctx context.Context, userID string, rc RequestContext) (Session, error) {
	suffix, err := passhash.RandomToken(32)
	if err != nil {
		return Session{}, err
	}
	token := "nrs_" + suffix
	id := uuid.NewString()
	ttl := s.cfg().SessionTTL
	if ttl <= 0 {
		ttl = defaultSessionTTL
	}
	expiresAt := isoMillis(time.Now().Add(ttl))
	ip, agent := rc.IPAddress, rc.UserAgent
	if _, err := s.DB.W.ExecContext(ctx,
		`INSERT INTO user_sessions (id, user_id, token_hash, expires_at, ip_address, user_agent) VALUES (?, ?, ?, ?, ?, ?)`,
		id, userID, passhash.DigestToken(token), expiresAt, nullString(&ip), nullString(&agent)); err != nil {
		return Session{}, fmt.Errorf("auth: create session: %w", err)
	}
	return Session{ID: id, Token: token, ExpiresAt: expiresAt}, nil
}

// AuthenticateToken resolves a bearer token to the account it belongs to: the
// session must exist, not be revoked or expired, and belong to an enabled
// account. The session's last_used_at is refreshed, as the JS service did.
func (s *Service) AuthenticateToken(ctx context.Context, token string) *Auth {
	if token == "" || !strings.HasPrefix(token, "nrs_") {
		return nil
	}
	var sessionID string
	var user userRow
	err := s.DB.R.QueryRowContext(ctx,
		`SELECT s.id, u.id, u.username, u.email, u.display_name, u.password_hash, u.role,
			u.disabled_at, u.created_at, u.updated_at, u.last_login_at
		FROM user_sessions s JOIN users u ON u.id = s.user_id
		WHERE s.token_hash = ? AND s.revoked_at IS NULL AND s.expires_at > ?`,
		passhash.DigestToken(token), isoMillis(time.Now())).
		Scan(&sessionID, &user.ID, &user.Username, &user.Email, &user.DisplayName, &user.PasswordHash,
			&user.Role, &user.DisabledAt, &user.CreatedAt, &user.UpdatedAt, &user.LastLoginAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		s.log().Error("auth: read session", "error", err)
		return nil
	}
	if user.DisabledAt.Valid {
		return nil
	}
	if _, err := s.DB.W.ExecContext(ctx,
		`UPDATE user_sessions SET last_used_at = strftime('%Y-%m-%dT%H:%M:%fZ','now') WHERE id = ?`, sessionID); err != nil {
		s.log().Error("auth: touch session", "error", err, "sessionId", sessionID)
	}
	return &Auth{Type: "session", SessionID: sessionID, UserID: user.ID, User: publicUser(&user)}
}

// Logout revokes one session.
func (s *Service) Logout(ctx context.Context, sessionID string) error {
	if _, err := s.DB.W.ExecContext(ctx,
		`UPDATE user_sessions SET revoked_at = strftime('%Y-%m-%dT%H:%M:%fZ','now') WHERE id = ?`, sessionID); err != nil {
		return fmt.Errorf("auth: revoke session: %w", err)
	}
	return nil
}

// logoutAll revokes every session of an account, used when its password changes
// and when an administrator disables it.
func (s *Service) logoutAll(ctx context.Context, q dbtx, userID string) error {
	if _, err := q.ExecContext(ctx,
		`UPDATE user_sessions SET revoked_at = strftime('%Y-%m-%dT%H:%M:%fZ','now') WHERE user_id = ? AND revoked_at IS NULL`,
		userID); err != nil {
		return fmt.Errorf("auth: revoke sessions: %w", err)
	}
	return nil
}

// LogoutAll revokes every session of an account.
func (s *Service) LogoutAll(ctx context.Context, userID string) error {
	return s.logoutAll(ctx, s.DB.W, userID)
}
