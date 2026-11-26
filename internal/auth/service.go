// Package auth owns NeoRouter's accounts: registration and the sign-in gate,
// bearer sessions (neolabs-go/sessionx), the authenticator-app second factor,
// passkeys, QR sign-in, password reset by mail, the per-account lockout and the
// session and admin guards the rest of the API is mounted behind.
//
// The package never imports another domain: the one thing it needs from
// elsewhere, whether registration is open, arrives through the narrow
// RegistrationGate interface the wiring satisfies.
package auth

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/NeoLabs-Systems/neolabs-go/dbx"
	"github.com/NeoLabs-Systems/neolabs-go/qrlogin"
	"github.com/NeoLabs-Systems/neolabs-go/servicemail"
	"github.com/NeoLabs-Systems/neolabs-go/sessionx"
	"github.com/NeoLabs-Systems/neorouter/internal/config"
	"github.com/NeoLabs-Systems/neorouter/internal/secrets"
)

// MinPassword is the shortest password the API accepts.
const MinPassword = 12

// RegistrationGate reports whether an administrator has opened registration for
// everybody. The settings domain satisfies it; a nil gate counts as closed.
type RegistrationGate interface {
	RegistrationOpen() bool
}

// Service owns accounts, sessions and the second factor. Dependencies are
// fields, so the app builds it with a struct literal.
type Service struct {
	DB  *dbx.DB
	Cfg func() config.Config
	Log *slog.Logger

	// Settings answers whether registration is open beyond the first account.
	Settings RegistrationGate
	// AccountData exports and deletes the caller's own account (settings domain).
	AccountData AccountData
	// BeforeDelete runs before an administrator deletes an account, while its
	// data is still there: the wiring leaves its pools.
	BeforeDelete func(ctx context.Context, userID string)
	// Secrets seals the second-factor secrets at rest. When nil the install's
	// secret.key is used.
	Secrets Secrets
	// Mail is the service mail setup (NEOROUTER_EMAIL_*). Password reset is
	// off unless it and its public URL are configured.
	Mail servicemail.Config
	// Mailer delivers mail; nil sends over SMTP with Mail.
	Mailer servicemail.Mailer
	// AllowedOrigins is the comma separated list of further browser origins
	// passkeys may be used from.
	AllowedOrigins string
	// Now overrides the clock the lockout and the authenticator codes read;
	// nil means time.Now.
	Now func() time.Time
	// HashCost is the bcrypt cost of new password and recovery code hashes;
	// zero means the defaults. Tests lower it.
	HashCost int

	mu           sync.Mutex
	sealerCache  Secrets
	sessionStore *sessionx.Store
	qrStore      *qrlogin.Store

	// attempts makes the lockout's check and record one step.
	attempts   sync.Mutex
	resetTries servicemail.AttemptLimiter
	sending    sync.WaitGroup

	challenges challengeStore
}

// Secrets seals and opens a value stored encrypted.
type Secrets interface {
	Seal(value string) (string, error)
	Open(value string) (string, error)
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

// hashCost is the bcrypt cost for a hash whose default is fallback.
func (s *Service) hashCost(fallback int) int {
	if s.HashCost > 0 {
		return s.HashCost
	}
	return fallback
}

func (s *Service) cfg() config.Config {
	if s.Cfg != nil {
		return s.Cfg()
	}
	return config.Config{}
}

func (s *Service) log() *slog.Logger {
	if s.Log != nil {
		return s.Log
	}
	return slog.Default()
}

// registrationOpen is the settings gate, closed unless something answers.
func (s *Service) registrationOpen() bool {
	return s.Settings != nil && s.Settings.RegistrationOpen()
}

// sealer returns the secret sealer, loading the install's key on first use.
func (s *Service) sealer() (Secrets, error) {
	if s.Secrets != nil {
		return s.Secrets, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.sealerCache != nil {
		return s.sealerCache, nil
	}
	sealer, err := secrets.FromFile(s.cfg().Paths.SecretKey)
	if err != nil {
		return nil, err
	}
	s.sealerCache = sealer
	return sealer, nil
}

// PublicUser is an account as the API returns it.
type PublicUser struct {
	ID          string  `json:"id"`
	Username    string  `json:"username"`
	Email       string  `json:"email"`
	DisplayName string  `json:"displayName"`
	Role        string  `json:"role"`
	Disabled    bool    `json:"disabled"`
	CreatedAt   string  `json:"createdAt"`
	LastLoginAt *string `json:"lastLoginAt"`
}

// Session is an issued bearer session. The token itself is returned once; only
// its digest is stored.
type Session struct {
	ID        string `json:"id"`
	Token     string `json:"token"`
	ExpiresAt string `json:"expiresAt"`
}

// LoginResult is a completed sign-in.
type LoginResult struct {
	User    PublicUser `json:"user"`
	Session Session    `json:"session"`
}

// RequestContext is the client metadata stored with a session.
type RequestContext struct {
	IPAddress string
	UserAgent string
}

// userRow is one row of the users table.
type userRow struct {
	ID           string
	Username     string
	Email        string
	DisplayName  sql.NullString
	PasswordHash string
	Role         string
	DisabledAt   sql.NullString
	CreatedAt    string
	UpdatedAt    string
	LastLoginAt  sql.NullString
}

const userColumns = `id, username, email, display_name, password_hash, role, disabled_at, created_at, updated_at, last_login_at`

type scanner interface {
	Scan(dest ...any) error
}

func scanUser(row scanner) (*userRow, error) {
	var user userRow
	err := row.Scan(&user.ID, &user.Username, &user.Email, &user.DisplayName, &user.PasswordHash,
		&user.Role, &user.DisabledAt, &user.CreatedAt, &user.UpdatedAt, &user.LastLoginAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("auth: read user: %w", err)
	}
	return &user, nil
}

// publicUser projects a users row for the API.
func publicUser(user *userRow) PublicUser {
	if user == nil {
		return PublicUser{}
	}
	name := user.Username
	if user.DisplayName.Valid && user.DisplayName.String != "" {
		name = user.DisplayName.String
	}
	return PublicUser{
		ID:          user.ID,
		Username:    user.Username,
		Email:       user.Email,
		DisplayName: name,
		Role:        user.Role,
		Disabled:    user.DisabledAt.Valid,
		CreatedAt:   user.CreatedAt,
		LastLoginAt: stringPtr(user.LastLoginAt),
	}
}

// findUser reads one account by id.
func (s *Service) findUser(ctx context.Context, q dbtx, userID string) (*userRow, error) {
	return scanUser(q.QueryRowContext(ctx, `SELECT `+userColumns+` FROM users WHERE id = ?`, userID))
}

// isoMillis formats t the way dates are stored: ISO 8601, always UTC with
// milliseconds.
func isoMillis(t time.Time) string {
	return t.UTC().Format("2006-01-02T15:04:05.000Z")
}

// jsLen is the length in UTF-16 code units, which is what the API's length
// rules are measured in.
func jsLen(value string) int {
	units := 0
	for _, r := range value {
		if r >= 0x10000 {
			units += 2
			continue
		}
		units++
	}
	return units
}

// truncRunes cuts value to at most n characters, applied to display names.
func truncRunes(value string, n int) string {
	runes := []rune(value)
	if len(runes) <= n {
		return value
	}
	return string(runes[:n])
}

func stringPtr(value sql.NullString) *string {
	if !value.Valid {
		return nil
	}
	out := value.String
	return &out
}

// dbtx is the part of database/sql shared by a pool and a transaction, so the
// same helpers serve a plain read and a transactional step.
type dbtx interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// bearerToken is the credential after "Bearer ".
func bearerToken(r *http.Request) string {
	value := r.Header.Get("Authorization")
	if !strings.HasPrefix(value, "Bearer ") {
		return ""
	}
	return strings.TrimSpace(strings.TrimPrefix(value, "Bearer "))
}
