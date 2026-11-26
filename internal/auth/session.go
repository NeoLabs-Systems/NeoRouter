package auth

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/NeoLabs-Systems/neolabs-go/passhash"
	"github.com/NeoLabs-Systems/neolabs-go/sessionx"
	"github.com/google/uuid"
)

const (
	// tokenPrefix starts every session token.
	tokenPrefix = "nrs_"
	// defaultSessionTTL applies when no configuration was supplied.
	defaultSessionTTL = 30 * 24 * time.Hour
)

func (s *Service) sessions(ctx context.Context) (*sessionx.Store, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.sessionStore == nil {
		store, err := sessionx.NewStore(ctx, s.DB.W)
		if err != nil {
			return nil, err
		}
		s.sessionStore = store
	}
	return s.sessionStore, nil
}

// createSession issues a session token; only its digest is stored.
func (s *Service) createSession(ctx context.Context, userID string, rc RequestContext) (Session, error) {
	store, err := s.sessions(ctx)
	if err != nil {
		return Session{}, err
	}
	if err := store.ClearExpired(ctx); err != nil {
		return Session{}, fmt.Errorf("auth: clear expired sessions: %w", err)
	}
	ttl := s.cfg().SessionTTL
	if ttl <= 0 {
		ttl = defaultSessionTTL
	}
	id := uuid.NewString()
	token, expires, err := store.IssueBearer(ctx, tokenPrefix, map[string]any{
		"id": id, "userId": userID, "ipAddress": rc.IPAddress, "userAgent": rc.UserAgent,
	}, ttl)
	if err != nil {
		return Session{}, fmt.Errorf("auth: create session: %w", err)
	}
	return Session{ID: id, Token: token, ExpiresAt: isoMillis(expires)}, nil
}

// AuthenticateToken resolves a bearer token to the account it belongs to: the
// session must be live and the account must exist and be enabled.
func (s *Service) AuthenticateToken(ctx context.Context, token string) *Auth {
	if !strings.HasPrefix(token, tokenPrefix) {
		return nil
	}
	store, err := s.sessions(ctx)
	if err != nil {
		s.log().Error("auth: open sessions", "error", err)
		return nil
	}
	sid, data, err := store.Bearer(ctx, token)
	if err != nil {
		s.log().Error("auth: read session", "error", err)
		return nil
	}
	userID, _ := data["userId"].(string)
	if userID == "" {
		return nil
	}
	user, err := s.findUser(ctx, s.DB.R, userID)
	if err != nil {
		s.log().Error("auth: read session account", "error", err)
		return nil
	}
	if user == nil || user.DisabledAt.Valid {
		return nil
	}
	return &Auth{Type: "session", SessionID: sid, UserID: user.ID, User: publicUser(user)}
}

// Logout ends one session, named by Auth.SessionID.
func (s *Service) Logout(ctx context.Context, sessionID string) error {
	store, err := s.sessions(ctx)
	if err != nil {
		return err
	}
	if err := store.Destroy(ctx, sessionID); err != nil {
		return fmt.Errorf("auth: end session: %w", err)
	}
	return nil
}

// LogoutAll ends every session of an account and withdraws what could still
// turn into one: a pending second-factor step and an approved QR sign-in.
func (s *Service) LogoutAll(ctx context.Context, userID string) error {
	s.dropChallenges(userID)
	if _, err := s.DB.W.ExecContext(ctx, `UPDATE user_qr_login_challenges SET status = 'declined'
		WHERE approved_by_user_id = ? AND status = 'approved'`, userID); err != nil {
		return fmt.Errorf("auth: withdraw approvals: %w", err)
	}
	store, err := s.sessions(ctx)
	if err != nil {
		return err
	}
	if err := store.DestroyForUser(ctx, userID); err != nil {
		return fmt.Errorf("auth: end sessions: %w", err)
	}
	return nil
}

// signIn issues a session for an account that has proved who it is.
func (s *Service) signIn(ctx context.Context, userID string, rc RequestContext) (LoginResult, error) {
	user, err := s.findUser(ctx, s.DB.R, userID)
	if err != nil {
		return LoginResult{}, err
	}
	if user == nil {
		return LoginResult{}, invalidCredentials()
	}
	if user.DisabledAt.Valid {
		return LoginResult{}, accountDisabled()
	}
	return s.completeLogin(ctx, user, rc)
}

// sessionDigest is how the approving session of a QR sign-in is recorded.
func sessionDigest(sessionID string) string { return passhash.DigestToken(sessionID) }
