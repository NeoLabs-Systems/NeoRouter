package teams

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/NeoLabs-Systems/neolabs-go/passhash"
	"github.com/NeoLabs-Systems/neorouter/internal/httpserver"
	"github.com/google/uuid"
)

// MaxActiveInvites is how many open invite links one account may hold.
const MaxActiveInvites = 25

const inviteQueryParam = "invite"

// Invite is one invite link as the Team page sees it. Only a SHA-256 of the
// token is stored; the raw token exists once, in the link handed to the owner.
type Invite struct {
	ID          string   `json:"id"`
	Label       string   `json:"label"`
	Permissions []string `json:"permissions"`
	SingleUse   bool     `json:"singleUse"`
	UseCount    int      `json:"useCount"`
	ExpiresAt   *string  `json:"expiresAt"`
	CreatedAt   string   `json:"createdAt"`
	Status      string   `json:"status"`
}

// InvitePreview is what the redeeming account is shown before joining.
type InvitePreview struct {
	Owner       *TeamUser `json:"owner"`
	Permissions []string  `json:"permissions"`
	ExpiresAt   *string   `json:"expiresAt"`
	SingleUse   bool      `json:"singleUse"`
}

type inviteRow struct {
	ID              string
	OwnerUserID     string
	Label           string
	PermissionsJSON string
	MaxUses         sql.NullInt64
	UseCount        int
	ExpiresAt       sql.NullString
	RevokedAt       sql.NullString
	CreatedAt       string
}

const inviteColumns = `id, owner_user_id, label, permissions_json, max_uses, use_count, expires_at, revoked_at, created_at`

type scanner interface {
	Scan(dest ...any) error
}

func scanInvite(row scanner) (*inviteRow, error) {
	var inv inviteRow
	err := row.Scan(&inv.ID, &inv.OwnerUserID, &inv.Label, &inv.PermissionsJSON, &inv.MaxUses,
		&inv.UseCount, &inv.ExpiresAt, &inv.RevokedAt, &inv.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("teams: read invite: %w", err)
	}
	return &inv, nil
}

// inviteStatus is revoked, expired, used or active, in that order of precedence.
func inviteStatus(row *inviteRow, now time.Time) string {
	if row.RevokedAt.Valid {
		return "revoked"
	}
	if row.ExpiresAt.Valid {
		if expiresAt, ok := parseISO(row.ExpiresAt.String); ok && !expiresAt.After(now) {
			return "expired"
		}
	}
	if row.MaxUses.Valid && int64(row.UseCount) >= row.MaxUses.Int64 {
		return "used"
	}
	return "active"
}

func publicInvite(row *inviteRow, now time.Time) Invite {
	return Invite{
		ID:          row.ID,
		Label:       row.Label,
		Permissions: Parse(row.PermissionsJSON),
		SingleUse:   row.MaxUses.Valid && row.MaxUses.Int64 == 1,
		UseCount:    row.UseCount,
		ExpiresAt:   stringPtr(row.ExpiresAt),
		CreatedAt:   row.CreatedAt,
		Status:      inviteStatus(row, now),
	}
}

// BuildLink is the address an invited person opens: the app page with the token
// in its query string.
func BuildLink(origin, token string) string {
	return strings.TrimRight(origin, "/") + "/app/?" + inviteQueryParam + "=" + token
}

var bareToken = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

// tokenFromLink accepts the full link or just the token, however it was pasted.
func tokenFromLink(link string) string {
	text := strings.TrimSpace(link)
	if text == "" {
		return ""
	}
	if parsed, err := url.Parse(text); err == nil && parsed.Scheme != "" {
		return parsed.Query().Get(inviteQueryParam)
	}
	if bareToken.MatchString(text) {
		return text
	}
	return ""
}

// ListInvites reads an owner's invite links, newest first.
func (s *Service) ListInvites(ctx context.Context, ownerUserID string) ([]Invite, error) {
	rows, err := s.DB.R.QueryContext(ctx,
		`SELECT `+inviteColumns+` FROM team_invites WHERE owner_user_id = ? ORDER BY created_at DESC`, ownerUserID)
	if err != nil {
		return nil, fmt.Errorf("teams: list invites: %w", err)
	}
	defer rows.Close()

	now := time.Now()
	invites := make([]Invite, 0)
	for rows.Next() {
		row, err := scanInvite(rows)
		if err != nil {
			return nil, err
		}
		invites = append(invites, publicInvite(row, now))
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("teams: list invites: %w", err)
	}
	return invites, nil
}

// CreateInvite mints an invite link carrying the granted rights and returns it
// with the one-time token that goes into the link.
func (s *Service) CreateInvite(ctx context.Context, ownerUserID, label string, granted []string, expiresInHours *int, singleUse bool) (Invite, string, error) {
	open, err := s.ListInvites(ctx, ownerUserID)
	if err != nil {
		return Invite{}, "", err
	}
	active := 0
	for _, invite := range open {
		if invite.Status == "active" {
			active++
		}
	}
	if active >= MaxActiveInvites {
		return Invite{}, "", httpserver.New(http.StatusTooManyRequests, "INVITE_LIMIT",
			fmt.Sprintf("You already have %d open invite links. Revoke some first.", MaxActiveInvites))
	}

	token, err := passhash.RandomToken(24)
	if err != nil {
		return Invite{}, "", err
	}
	id := uuid.NewString()
	scope := Normalize(granted)
	var expiresAt *string
	if expiresInHours != nil {
		value := isoMillis(time.Now().Add(time.Duration(*expiresInHours) * time.Hour))
		expiresAt = &value
	}
	var maxUses any
	if singleUse {
		maxUses = 1
	}

	tx, err := s.DB.W.BeginTx(ctx, nil)
	if err != nil {
		return Invite{}, "", fmt.Errorf("teams: begin: %w", err)
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx,
		`INSERT INTO team_invites (id, owner_user_id, token_hash, label, permissions_json, max_uses, expires_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		id, ownerUserID, passhash.DigestToken(token), truncRunes(strings.TrimSpace(label), 80), encodeStringArray(scope),
		maxUses, nullString(expiresAt)); err != nil {
		return Invite{}, "", fmt.Errorf("teams: create invite: %w", err)
	}
	if err := s.recordAudit(ctx, tx, "invite.create", ownerUserID, nil, mustJSON(inviteCreateDetail{
		InviteID:    id,
		Permissions: scope,
		SingleUse:   singleUse,
		ExpiresAt:   expiresAt,
	})); err != nil {
		return Invite{}, "", err
	}
	row, err := scanInvite(tx.QueryRowContext(ctx, `SELECT `+inviteColumns+` FROM team_invites WHERE id = ?`, id))
	if err != nil {
		return Invite{}, "", err
	}
	if err := tx.Commit(); err != nil {
		return Invite{}, "", fmt.Errorf("teams: commit invite: %w", err)
	}
	return publicInvite(row, time.Now()), token, nil
}

// RevokeInvite closes an invite link so nobody can redeem it any more.
func (s *Service) RevokeInvite(ctx context.Context, ownerUserID, inviteID string) error {
	tx, err := s.DB.W.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("teams: begin: %w", err)
	}
	defer tx.Rollback()

	result, err := tx.ExecContext(ctx,
		`UPDATE team_invites SET revoked_at = strftime('%Y-%m-%dT%H:%M:%fZ','now')
		WHERE id = ? AND owner_user_id = ? AND revoked_at IS NULL`, inviteID, ownerUserID)
	if err != nil {
		return fmt.Errorf("teams: revoke invite: %w", err)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("teams: revoke invite: %w", err)
	}
	if changed == 0 {
		return httpserver.New(http.StatusNotFound, "INVITE_NOT_FOUND", "This invite link does not exist.")
	}
	if err := s.recordAudit(ctx, tx, "invite.revoke", ownerUserID, nil, mustJSON(inviteRevokeDetail{InviteID: inviteID})); err != nil {
		return err
	}
	return tx.Commit()
}

// checkRedeemable looks a link up and refuses it unless it can still be used by
// this account. It runs for the preview and again inside the redeeming
// transaction, so a link that changed in between is caught.
func (s *Service) checkRedeemable(ctx context.Context, q dbtx, userID, link string) (*inviteRow, error) {
	token := tokenFromLink(link)
	var row *inviteRow
	var err error
	if token != "" {
		row, err = scanInvite(q.QueryRowContext(ctx,
			`SELECT `+inviteColumns+` FROM team_invites WHERE token_hash = ?`, passhash.DigestToken(token)))
		if err != nil {
			return nil, err
		}
	}
	if row == nil {
		return nil, httpserver.New(http.StatusNotFound, "INVITE_INVALID", "This invite link isn’t valid.")
	}
	switch inviteStatus(row, time.Now()) {
	case "revoked":
		return nil, httpserver.New(http.StatusGone, "INVITE_REVOKED", "This invite link was revoked.")
	case "expired":
		return nil, httpserver.New(http.StatusGone, "INVITE_EXPIRED", "This invite link has expired.")
	case "used":
		return nil, httpserver.New(http.StatusGone, "INVITE_USED", "This invite link has already been used.")
	}
	if err := s.assertCanJoin(ctx, q, row.OwnerUserID, userID); err != nil {
		return nil, err
	}
	return row, nil
}

// PreviewInvite shows what redeeming a link would give, without joining.
func (s *Service) PreviewInvite(ctx context.Context, userID, link string) (InvitePreview, error) {
	row, err := s.checkRedeemable(ctx, s.DB.R, userID, link)
	if err != nil {
		return InvitePreview{}, err
	}
	owner, err := s.teamUser(ctx, s.DB.R, row.OwnerUserID)
	if err != nil {
		return InvitePreview{}, err
	}
	return InvitePreview{
		Owner:       owner,
		Permissions: Parse(row.PermissionsJSON),
		ExpiresAt:   stringPtr(row.ExpiresAt),
		SingleUse:   row.MaxUses.Valid && row.MaxUses.Int64 == 1,
	}, nil
}

// RedeemInvite adds the caller to the owner's team and uses up one use of the
// link, in one transaction.
func (s *Service) RedeemInvite(ctx context.Context, userID, link string) error {
	tx, err := s.DB.W.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("teams: begin: %w", err)
	}
	defer tx.Rollback()

	row, err := s.checkRedeemable(ctx, tx, userID, link)
	if err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx,
		`UPDATE team_invites SET use_count = use_count + 1
		WHERE id = ? AND revoked_at IS NULL AND (max_uses IS NULL OR use_count < max_uses)`, row.ID)
	if err != nil {
		return fmt.Errorf("teams: redeem invite: %w", err)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("teams: redeem invite: %w", err)
	}
	if changed == 0 {
		return httpserver.New(http.StatusGone, "INVITE_USED", "This invite link has already been used.")
	}
	if err := s.addMember(ctx, tx, row.OwnerUserID, userID, Parse(row.PermissionsJSON), row.ID); err != nil {
		return err
	}
	return tx.Commit()
}
