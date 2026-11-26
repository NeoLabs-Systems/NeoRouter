package auth

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/NeoLabs-Systems/neolabs-go/lockout"
	"github.com/NeoLabs-Systems/neolabs-go/passhash"
	"github.com/NeoLabs-Systems/neolabs-go/servicemail"
	"github.com/NeoLabs-Systems/neolabs-go/userid"
	"github.com/NeoLabs-Systems/neorouter/internal/httpserver"
)

// Password reset by service mail (neolabs-go/servicemail and authpages). It is
// off unless mail and a public URL are configured: the link is never built from
// the request's Host header, which the person asking could forge.

const (
	passwordResetPath  = "/api/v1/auth/password/reset"
	tokenPasswordReset = "password_reset"
	mailTimeout        = 30 * time.Second
)

// PasswordResetEnabled reports whether reset links can be sent.
func (s *Service) PasswordResetEnabled() bool {
	return s.Mail.Enabled && s.Mail.PublicURL != ""
}

func (s *Service) mailTokens() servicemail.Tokens { return servicemail.Tokens{DB: s.DB.W} }

func (s *Service) mailer() servicemail.Mailer {
	if s.Mailer != nil {
		return s.Mailer
	}
	return servicemail.Sender{Cfg: s.Mail}
}

// requestPasswordReset mails a reset link to the account's address, if there is
// such an enabled account.
func (s *Service) requestPasswordReset(ctx context.Context, account string) error {
	if !s.PasswordResetEnabled() || strings.TrimSpace(account) == "" {
		return nil
	}
	user, err := s.userByAccount(ctx, account)
	if err != nil || user == nil || user.DisabledAt.Valid || user.Email == "" {
		return err
	}
	ttl := time.Duration(s.Mail.TokenTTLHours * float64(time.Hour))
	token, _, err := s.mailTokens().Create(ctx, userid.ID(user.ID), tokenPasswordReset, user.Email, ttl)
	if err != nil {
		return err
	}
	return s.mailer().Send(ctx, user.Email, servicemail.Message{
		Subject: "Reset your NeoRouter password", Title: "Reset your password",
		Intro:     "Use this link to set a new password for " + user.Username + ". Ignore this email if you did not request a reset.",
		ActionURL: s.Mail.PublicURL + passwordResetPath + "?token=" + url.QueryEscape(token), ActionLabel: "Reset password",
		Details: []servicemail.Detail{{Label: "Account", Value: user.Username}},
	})
}

// sendResetInBackground keeps the answer's timing independent of whether the
// account exists.
func (s *Service) sendResetInBackground(ctx context.Context, account string) {
	s.sending.Add(1)
	go func(ctx context.Context) {
		defer s.sending.Done()
		ctx, cancel := context.WithTimeout(ctx, mailTimeout)
		defer cancel()
		if err := s.requestPasswordReset(ctx, account); err != nil {
			s.log().Error("auth: password reset email failed", "error", err)
		}
	}(context.WithoutCancel(ctx))
}

// Wait blocks until the mails being sent are done.
func (s *Service) Wait() { s.sending.Wait() }

func invalidResetLink() error {
	return httpserver.New(http.StatusBadRequest, "INVALID_RESET_LINK", "Password reset link is invalid or expired.")
}

// resetPassword spends a reset link on a new password, which signs the account
// out everywhere and lifts its lockout.
func (s *Service) resetPassword(ctx context.Context, token, password string) error {
	key := passhash.DigestToken(token)
	if err := s.resetTries.Allowed(key); err != nil {
		return httpserver.New(http.StatusTooManyRequests, "RATE_LIMITED", err.Error())
	}
	row, err := s.mailTokens().Find(ctx, s.DB.W, token, tokenPasswordReset)
	if err != nil {
		return err
	}
	if row == nil {
		s.resetTries.Register(key)
		return invalidResetLink()
	}
	user, err := s.findUser(ctx, s.DB.R, string(row.UserID))
	if err != nil {
		return err
	}
	if user == nil || user.DisabledAt.Valid {
		return invalidResetLink()
	}
	// Check the rules before spending the link, so a weak password can be retried.
	if err := ValidateNewPassword(password, user.Username, user.Email); err != nil {
		return err
	}
	result, err := s.DB.W.ExecContext(ctx,
		`UPDATE user_email_tokens SET consumed_at = datetime('now') WHERE id = ? AND consumed_at IS NULL`, row.ID)
	if err != nil {
		return err
	}
	if changed, err := result.RowsAffected(); err != nil || changed != 1 {
		return invalidResetLink()
	}
	if err := s.setPassword(ctx, user.ID, password); err != nil {
		return err
	}
	store := lockout.Store{DB: s.DB.W}
	for _, lockKey := range []string{lockout.ConfirmKey(user.ID), signInKey(user.ID)} {
		if err := store.Clear(ctx, lockKey, ""); err != nil {
			return err
		}
	}
	s.resetTries.Reset(key)
	return nil
}
