package auth

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/NeoLabs-Systems/neolabs-go/passhash"
	"github.com/NeoLabs-Systems/neorouter/internal/httpserver"
	"github.com/google/uuid"
)

// RegisterInput is one registration request.
type RegisterInput struct {
	Username    string
	Email       string
	Password    string
	DisplayName *string
}

// RegistrationStatus is the GET /auth/registration body.
type RegistrationStatus struct {
	Open      bool `json:"open"`
	Bootstrap bool `json:"bootstrap"`
}

// LoginOutcome is either a completed sign-in or a pending second-factor
// challenge, which is what the login endpoint answers with when 2FA is on.
type LoginOutcome struct {
	TwoFactorRequired bool
	Challenge         string
	Result            LoginResult
}

// hasUsers reports whether any account exists, which is what makes the next
// registration the bootstrap administrator.
func (s *Service) hasUsers(ctx context.Context, q dbtx) (bool, error) {
	var one int
	err := q.QueryRowContext(ctx, `SELECT 1 FROM users LIMIT 1`).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("auth: count users: %w", err)
	}
	return true, nil
}

// RegistrationStatus reports whether registration is open, and whether the
// next account becomes the instance administrator.
func (s *Service) RegistrationStatus(ctx context.Context) (RegistrationStatus, error) {
	existing, err := s.hasUsers(ctx, s.DB.R)
	if err != nil {
		return RegistrationStatus{}, err
	}
	bootstrap := !existing
	return RegistrationStatus{Open: bootstrap || s.registrationOpen(), Bootstrap: bootstrap}, nil
}

// assertPasswordLength is the service-side password rule, applied after the
// request validation so both layers keep the JS texts.
func assertPasswordLength(password string) error {
	if jsLen(password) < MinPassword {
		return httpserver.New(http.StatusBadRequest, "WEAK_PASSWORD",
			fmt.Sprintf("Password must contain at least %d characters.", MinPassword))
	}
	return nil
}

// Register creates an account and signs it in. The first account ever made is
// the administrator; the registration gate is evaluated inside the transaction
// so two concurrent bootstrap requests cannot both become admin.
func (s *Service) Register(ctx context.Context, in RegisterInput, rc RequestContext) (LoginResult, error) {
	username := strings.TrimSpace(in.Username)
	email := strings.ToLower(strings.TrimSpace(in.Email))
	if length := jsLen(username); length < 3 || length > 64 {
		return LoginResult{}, httpserver.New(http.StatusBadRequest, "INVALID_USERNAME",
			"Username must contain 3 to 64 characters.")
	}
	if email == "" {
		return LoginResult{}, httpserver.New(http.StatusBadRequest, "EMAIL_REQUIRED", "An email address is required.")
	}
	if err := assertPasswordLength(in.Password); err != nil {
		return LoginResult{}, err
	}
	passwordHash, err := passhash.Hash(in.Password, passhash.DefaultCost)
	if err != nil {
		return LoginResult{}, err
	}
	displayName := username
	if in.DisplayName != nil && *in.DisplayName != "" {
		displayName = *in.DisplayName
	}
	id := uuid.NewString()

	tx, err := s.DB.W.BeginTx(ctx, nil)
	if err != nil {
		return LoginResult{}, fmt.Errorf("auth: begin: %w", err)
	}
	defer tx.Rollback()

	existing, err := s.hasUsers(ctx, tx)
	if err != nil {
		return LoginResult{}, err
	}
	bootstrap := !existing
	if !bootstrap && !s.registrationOpen() {
		return LoginResult{}, httpserver.New(http.StatusForbidden, "REGISTRATION_DISABLED",
			"Registration is disabled. Ask an administrator to open it.")
	}
	var taken int
	err = tx.QueryRowContext(ctx, `SELECT 1 FROM users WHERE username = ?`, username).Scan(&taken)
	if err == nil {
		return LoginResult{}, httpserver.New(http.StatusConflict, "USERNAME_TAKEN", "This username is already in use.")
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return LoginResult{}, fmt.Errorf("auth: check username: %w", err)
	}
	err = tx.QueryRowContext(ctx, `SELECT 1 FROM users WHERE email = ?`, email).Scan(&taken)
	if err == nil {
		return LoginResult{}, httpserver.New(http.StatusConflict, "EMAIL_TAKEN", "This email is already in use.")
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return LoginResult{}, fmt.Errorf("auth: check email: %w", err)
	}
	role := "user"
	if bootstrap {
		role = "admin"
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO users (id, username, email, display_name, password_hash, role) VALUES (?, ?, ?, ?, ?, ?)`,
		id, username, email, displayName, passwordHash, role); err != nil {
		return LoginResult{}, fmt.Errorf("auth: create user: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return LoginResult{}, fmt.Errorf("auth: commit user: %w", err)
	}

	user, err := s.findUser(ctx, s.DB.R, id)
	if err != nil {
		return LoginResult{}, err
	}
	session, err := s.createSession(ctx, id, rc)
	if err != nil {
		return LoginResult{}, err
	}
	return LoginResult{User: publicUser(user), Session: session}, nil
}

// Login checks a username or email and its password. With the second factor on,
// the password only earns a short-lived challenge; the session is created once
// the code is verified.
func (s *Service) Login(ctx context.Context, account, password string, rc RequestContext) (LoginOutcome, error) {
	lookup := strings.TrimSpace(account)
	user, err := scanUser(s.DB.R.QueryRowContext(ctx,
		`SELECT `+userColumns+` FROM users WHERE username = ? OR email = ?`, lookup, lookup))
	if err != nil {
		return LoginOutcome{}, err
	}
	valid := user != nil && passhash.Verify(user.PasswordHash, password)
	if !valid {
		return LoginOutcome{}, httpserver.New(http.StatusUnauthorized, "INVALID_CREDENTIALS",
			"Username or password is incorrect.")
	}
	if user.DisabledAt.Valid {
		return LoginOutcome{}, httpserver.New(http.StatusForbidden, "ACCOUNT_DISABLED", "This account is disabled.")
	}
	if s.twoFactorEnabled(ctx, user.ID) {
		challenge, err := s.createChallenge(user.ID)
		if err != nil {
			return LoginOutcome{}, err
		}
		return LoginOutcome{TwoFactorRequired: true, Challenge: challenge}, nil
	}
	result, err := s.completeLogin(ctx, user, rc)
	if err != nil {
		return LoginOutcome{}, err
	}
	return LoginOutcome{Result: result}, nil
}

// completeLogin stamps the sign-in and issues the session. The user projection
// is the row read before the stamp, exactly as the JS service returned it.
func (s *Service) completeLogin(ctx context.Context, user *userRow, rc RequestContext) (LoginResult, error) {
	if _, err := s.DB.W.ExecContext(ctx,
		`UPDATE users SET last_login_at = strftime('%Y-%m-%dT%H:%M:%fZ','now') WHERE id = ?`, user.ID); err != nil {
		return LoginResult{}, fmt.Errorf("auth: stamp login: %w", err)
	}
	session, err := s.createSession(ctx, user.ID, rc)
	if err != nil {
		return LoginResult{}, err
	}
	return LoginResult{User: publicUser(user), Session: session}, nil
}

// LoginWithTwoFactor finishes a sign-in: the challenge names the account and the
// code proves the second factor. A challenge works once.
func (s *Service) LoginWithTwoFactor(ctx context.Context, challenge, code string, rc RequestContext) (LoginResult, error) {
	userID, err := s.consumeChallenge(challenge)
	if err != nil {
		return LoginResult{}, err
	}
	valid, err := s.verifyCode(ctx, userID, code)
	if err != nil {
		return LoginResult{}, err
	}
	if !valid {
		return LoginResult{}, httpserver.New(http.StatusUnauthorized, "INVALID_TWO_FACTOR", "The code is not valid.")
	}
	s.finishChallenge(challenge)
	user, err := s.findUser(ctx, s.DB.R, userID)
	if err != nil {
		return LoginResult{}, err
	}
	if user == nil || user.DisabledAt.Valid {
		return LoginResult{}, httpserver.New(http.StatusForbidden, "ACCOUNT_DISABLED", "This account is disabled.")
	}
	return s.completeLogin(ctx, user, rc)
}

// ChangePassword replaces the password of a signed-in account after asking for
// the current one again, and signs the account out everywhere.
func (s *Service) ChangePassword(ctx context.Context, userID, currentPassword, newPassword string) error {
	if err := s.assertPassword(ctx, userID, currentPassword); err != nil {
		return err
	}
	if err := assertPasswordLength(newPassword); err != nil {
		return err
	}
	passwordHash, err := passhash.Hash(newPassword, passhash.DefaultCost)
	if err != nil {
		return err
	}
	tx, err := s.DB.W.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("auth: begin: %w", err)
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx,
		`UPDATE users SET password_hash = ?, updated_at = strftime('%Y-%m-%dT%H:%M:%fZ','now') WHERE id = ?`,
		passwordHash, userID); err != nil {
		return fmt.Errorf("auth: update password: %w", err)
	}
	if err := s.logoutAll(ctx, tx, userID); err != nil {
		return err
	}
	return tx.Commit()
}

// ProfileUpdate is a PATCH /auth/profile body: nil means "leave as it is".
type ProfileUpdate struct {
	DisplayName *string
	Email       *string
}

// UpdateProfile changes the display name and email of an account.
func (s *Service) UpdateProfile(ctx context.Context, userID string, update ProfileUpdate) (PublicUser, error) {
	user, err := s.findUser(ctx, s.DB.R, userID)
	if err != nil {
		return PublicUser{}, err
	}
	if user == nil {
		return PublicUser{}, httpserver.New(http.StatusNotFound, "USER_NOT_FOUND", "This account does not exist.")
	}
	email := user.Email
	if update.Email != nil {
		email = strings.ToLower(strings.TrimSpace(*update.Email))
	}
	var taken int
	err = s.DB.R.QueryRowContext(ctx, `SELECT 1 FROM users WHERE email = ? AND id != ?`, email, userID).Scan(&taken)
	if err == nil {
		return PublicUser{}, httpserver.New(http.StatusConflict, "EMAIL_TAKEN", "This email is already in use.")
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return PublicUser{}, fmt.Errorf("auth: check email: %w", err)
	}
	displayName := user.DisplayName
	if update.DisplayName != nil {
		name := truncRunes(strings.TrimSpace(*update.DisplayName), 80)
		if name == "" {
			name = user.Username
		}
		displayName = sql.NullString{String: name, Valid: true}
	}
	if _, err := s.DB.W.ExecContext(ctx,
		`UPDATE users SET display_name = ?, email = ?, updated_at = strftime('%Y-%m-%dT%H:%M:%fZ','now') WHERE id = ?`,
		displayName, email, userID); err != nil {
		return PublicUser{}, fmt.Errorf("auth: update profile: %w", err)
	}
	updated, err := s.findUser(ctx, s.DB.R, userID)
	if err != nil {
		return PublicUser{}, err
	}
	return publicUser(updated), nil
}
