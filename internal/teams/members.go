package teams

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"

	"github.com/NeoLabs-Systems/neorouter/internal/httpserver"
)

// TeamUser is the only account projection teammates see about each other.
type TeamUser struct {
	ID          string `json:"id"`
	Username    string `json:"username"`
	DisplayName string `json:"displayName"`
}

// Membership says whose team an account is in and what it may do there.
type Membership struct {
	OwnerUserID string
	Permissions []string
	CreatedAt   string
}

// Member is one row of an owner's team as the API returns it.
type Member struct {
	User        *TeamUser `json:"user"`
	Permissions []string  `json:"permissions"`
	JoinedAt    string    `json:"joinedAt"`
}

// teamUser reads one account for display, or nil when it does not exist.
func (s *Service) teamUser(ctx context.Context, q dbtx, userID string) (*TeamUser, error) {
	if userID == "" {
		return nil, nil
	}
	var id, username string
	var displayName sql.NullString
	err := q.QueryRowContext(ctx, `SELECT id, username, display_name FROM users WHERE id = ?`, userID).
		Scan(&id, &username, &displayName)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("teams: read user: %w", err)
	}
	name := username
	if displayName.Valid && displayName.String != "" {
		name = displayName.String
	}
	return &TeamUser{ID: id, Username: username, DisplayName: name}, nil
}

// TeamUser is the exported form of teamUser, for the admin user list.
func (s *Service) TeamUser(ctx context.Context, userID string) (*TeamUser, error) {
	return s.teamUser(ctx, s.DB.R, userID)
}

// membership reports the team an account belongs to, or nil when it is on its
// own. Membership is one team per account: team_members is keyed by member.
func (s *Service) membership(ctx context.Context, q dbtx, memberUserID string) (*Membership, error) {
	var ownerUserID, permissionsJSON, createdAt string
	err := q.QueryRowContext(ctx,
		`SELECT owner_user_id, permissions_json, created_at FROM team_members WHERE member_user_id = ?`,
		memberUserID).Scan(&ownerUserID, &permissionsJSON, &createdAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("teams: read membership: %w", err)
	}
	return &Membership{OwnerUserID: ownerUserID, Permissions: Parse(permissionsJSON), CreatedAt: createdAt}, nil
}

// Membership is the exported form of membership, for the admin user list and
// for other domains that need to know who an account shares with.
func (s *Service) Membership(ctx context.Context, userID string) (*Membership, error) {
	return s.membership(ctx, s.DB.R, userID)
}

// listMembers reads an owner's team in join order.
func (s *Service) listMembers(ctx context.Context, q dbtx, ownerUserID string) ([]Member, error) {
	rows, err := q.QueryContext(ctx,
		`SELECT member_user_id, permissions_json, created_at FROM team_members WHERE owner_user_id = ? ORDER BY created_at`,
		ownerUserID)
	if err != nil {
		return nil, fmt.Errorf("teams: list members: %w", err)
	}
	defer rows.Close()

	members := make([]Member, 0)
	type pending struct {
		id, permissionsJSON, createdAt string
	}
	var found []pending
	for rows.Next() {
		var row pending
		if err := rows.Scan(&row.id, &row.permissionsJSON, &row.createdAt); err != nil {
			return nil, fmt.Errorf("teams: read member: %w", err)
		}
		found = append(found, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("teams: list members: %w", err)
	}
	rows.Close()
	for _, row := range found {
		user, err := s.teamUser(ctx, q, row.id)
		if err != nil {
			return nil, err
		}
		members = append(members, Member{User: user, Permissions: Parse(row.permissionsJSON), JoinedAt: row.createdAt})
	}
	return members, nil
}

// assertCanJoin refuses a join by someone who is already in a team: membership
// is exclusive, so the person has to leave their team first.
func (s *Service) assertCanJoin(ctx context.Context, q dbtx, ownerUserID, memberUserID string) error {
	if ownerUserID == memberUserID {
		return httpserver.New(http.StatusBadRequest, "INVITE_SELF", "You can’t use your own invite link.")
	}
	current, err := s.membership(ctx, q, memberUserID)
	if err != nil {
		return err
	}
	if current == nil {
		return nil
	}
	owner, err := s.teamUser(ctx, q, current.OwnerUserID)
	if err != nil {
		return err
	}
	name := "another"
	if owner != nil {
		name = owner.DisplayName
	}
	return httpserver.New(http.StatusConflict, "ALREADY_MEMBER",
		"You are already in "+name+" team. Leave it first.")
}

// addMember puts memberUserID in ownerUserID's team with the granted rights and
// records the join in the audit trail.
func (s *Service) addMember(ctx context.Context, tx dbtx, ownerUserID, memberUserID string, granted []string, inviteID string) error {
	if err := s.assertCanJoin(ctx, tx, ownerUserID, memberUserID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO team_members (member_user_id, owner_user_id, permissions_json, invite_id) VALUES (?, ?, ?, ?)`,
		memberUserID, ownerUserID, encodeStringArray(Normalize(granted)), inviteID); err != nil {
		return fmt.Errorf("teams: add member: %w", err)
	}
	return s.recordAudit(ctx, tx, "member.join", memberUserID, &ownerUserID,
		mustJSON(memberJoinDetail{InviteID: inviteID}))
}

// requireMember returns the membership of a member of ownerUserID's team.
func (s *Service) requireMember(ctx context.Context, q dbtx, ownerUserID, memberUserID string) (*Membership, error) {
	current, err := s.membership(ctx, q, memberUserID)
	if err != nil {
		return nil, err
	}
	if current == nil || current.OwnerUserID != ownerUserID {
		return nil, httpserver.New(http.StatusNotFound, "MEMBER_NOT_FOUND", "This person is not in your team.")
	}
	return current, nil
}

// SetPermissions replaces what one member may do in the owner's space.
func (s *Service) SetPermissions(ctx context.Context, ownerUserID, memberUserID string, granted []string) error {
	tx, err := s.DB.W.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("teams: begin: %w", err)
	}
	defer tx.Rollback()

	if _, err := s.requireMember(ctx, tx, ownerUserID, memberUserID); err != nil {
		return err
	}
	next := Normalize(granted)
	if _, err := tx.ExecContext(ctx,
		`UPDATE team_members SET permissions_json = ? WHERE member_user_id = ?`,
		encodeStringArray(next), memberUserID); err != nil {
		return fmt.Errorf("teams: set permissions: %w", err)
	}
	if err := s.recordAudit(ctx, tx, "member.permissions", ownerUserID, &memberUserID,
		mustJSON(memberPermissionsDetail{Permissions: next})); err != nil {
		return err
	}
	return tx.Commit()
}

// ReleaseMember removes a member from the owner's team.
func (s *Service) ReleaseMember(ctx context.Context, ownerUserID, memberUserID string) error {
	tx, err := s.DB.W.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("teams: begin: %w", err)
	}
	defer tx.Rollback()

	if _, err := s.requireMember(ctx, tx, ownerUserID, memberUserID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM team_members WHERE member_user_id = ?`, memberUserID); err != nil {
		return fmt.Errorf("teams: release member: %w", err)
	}
	if err := s.recordAudit(ctx, tx, "member.release", ownerUserID, &memberUserID, emptyDetail); err != nil {
		return err
	}
	return tx.Commit()
}

// Leave takes the caller out of the team it is in.
func (s *Service) Leave(ctx context.Context, memberUserID string) error {
	tx, err := s.DB.W.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("teams: begin: %w", err)
	}
	defer tx.Rollback()

	current, err := s.membership(ctx, tx, memberUserID)
	if err != nil {
		return err
	}
	if current == nil {
		return httpserver.New(http.StatusNotFound, "NOT_A_MEMBER", "You are not in a team.")
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM team_members WHERE member_user_id = ?`, memberUserID); err != nil {
		return fmt.Errorf("teams: leave team: %w", err)
	}
	ownerUserID := current.OwnerUserID
	if err := s.recordAudit(ctx, tx, "member.leave", memberUserID, &ownerUserID, emptyDetail); err != nil {
		return err
	}
	return tx.Commit()
}
