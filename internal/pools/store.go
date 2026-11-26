package pools

// Database access for pools, members, invites, shared connections and partner
// links. A person may be in several pools; everything is keyed by (person,
// pool). No logic beyond reading and writing rows lives here.

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

func isNoRows(err error) bool { return errors.Is(err, sql.ErrNoRows) }

// pool is one row of pools as the rest of the domain reads it.
type pool struct {
	UserID        string
	PoolID        string
	Name          string
	IsHost        bool
	HostMemberID  string
	HostPublicKey string
	HostURL       string
	SelfURL       string
	SelfName      string
	Share         map[string]any
	Paused        bool
	// RelayEnabled: host side, this pool's traffic may pass through us.
	// HostRelay: members' side, our host offers that.
	RelayEnabled bool
	HostRelay    bool
	JoinedAt     string
	LastSyncAt   *string
}

// poolRow reads one pools row.
func scanPool(scan func(...any) error) (*pool, error) {
	var (
		row      pool
		isHost   int
		paused   int
		relay    int
		hostRely int
		share    string
		lastSync *string
	)
	if err := scan(&row.UserID, &row.PoolID, &row.Name, &isHost, &row.HostMemberID, &row.HostPublicKey,
		&row.HostURL, &row.SelfURL, &row.SelfName, &share, &paused, &row.JoinedAt, &lastSync,
		&relay, &hostRely); err != nil {
		if isNoRows(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("pools: read pool: %w", err)
	}
	row.IsHost = isHost == 1
	row.Paused = paused == 1
	row.RelayEnabled = relay != 0
	row.HostRelay = hostRely == 1
	row.LastSyncAt = lastSync
	row.Share = map[string]any{}
	if share != "" {
		_ = json.Unmarshal([]byte(share), &row.Share)
	}
	if row.Share == nil {
		row.Share = map[string]any{}
	}
	return &row, nil
}

const poolColumns = `user_id, pool_id, name, is_host, host_member_id, host_public_key, host_url, self_url,
	self_name, share_json, paused, joined_at, last_sync_at, relay_enabled, host_relay`

// pool reads one person's pool, nil when they are not in it.
func (s *Service) pool(ctx context.Context, userID, poolID string) (*pool, error) {
	row := s.DB.R.QueryRowContext(ctx, `SELECT `+poolColumns+` FROM pools WHERE user_id = ? AND pool_id = ?`,
		userID, poolID)
	return scanPool(row.Scan)
}

// pools lists one person's pools, in the order they joined them.
func (s *Service) pools(ctx context.Context, userID string) ([]*pool, error) {
	rows, err := s.DB.R.QueryContext(ctx, `SELECT `+poolColumns+` FROM pools WHERE user_id = ? ORDER BY joined_at, rowid`, userID)
	if err != nil {
		return nil, fmt.Errorf("pools: list pools: %w", err)
	}
	defer rows.Close()
	list := []*pool{}
	for rows.Next() {
		row, err := scanPool(rows.Scan)
		if err != nil {
			return nil, err
		}
		if row != nil {
			list = append(list, row)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("pools: list pools: %w", err)
	}
	return list, nil
}

// hostedPool is the pool this instance hosts with that id: relay connections
// name only the pool.
func (s *Service) hostedPool(ctx context.Context, poolID string) (*pool, error) {
	row := s.DB.R.QueryRowContext(ctx, `SELECT `+poolColumns+` FROM pools WHERE pool_id = ? AND is_host = 1`, poolID)
	return scanPool(row.Scan)
}

// poolInput is a new pool row.
type poolInput struct {
	UserID        string
	PoolID        string
	Name          string
	IsHost        bool
	HostMemberID  string
	HostPublicKey string
	HostURL       string
	SelfURL       string
	SelfName      string
	Share         map[string]any
	HostRelay     bool
}

func (s *Service) insertPool(ctx context.Context, value poolInput) error {
	share := value.Share
	if share == nil {
		share = map[string]any{}
	}
	encoded, err := json.Marshal(share)
	if err != nil {
		return fmt.Errorf("pools: encode the share: %w", err)
	}
	_, err = s.DB.W.ExecContext(ctx, `INSERT INTO pools (user_id, pool_id, name, is_host, host_member_id,
		host_public_key, host_url, self_url, self_name, share_json, host_relay) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		value.UserID, value.PoolID, value.Name, boolInt(value.IsHost), value.HostMemberID, value.HostPublicKey,
		value.HostURL, value.SelfURL, value.SelfName, string(encoded), boolInt(value.HostRelay))
	if err != nil {
		return fmt.Errorf("pools: insert pool: %w", err)
	}
	return nil
}

// poolUpdate is the set of pool columns a change may touch; a nil field is left
// alone.
type poolUpdate struct {
	Name         *string
	SelfName     *string
	SelfURL      *string
	Share        map[string]any
	LastSyncAt   *string
	Paused       *bool
	RelayEnabled *bool
	HostRelay    *bool
}

func (s *Service) updatePool(ctx context.Context, userID, poolID string, fields poolUpdate) error {
	steps := []struct {
		column string
		value  any
		ok     bool
	}{
		{"name", nil, fields.Name != nil},
		{"self_name", nil, fields.SelfName != nil},
		{"self_url", nil, fields.SelfURL != nil},
		{"share_json", nil, fields.Share != nil},
		{"last_sync_at", nil, fields.LastSyncAt != nil},
		{"paused", nil, fields.Paused != nil},
		{"relay_enabled", nil, fields.RelayEnabled != nil},
		{"host_relay", nil, fields.HostRelay != nil},
	}
	if fields.Name != nil {
		steps[0].value = *fields.Name
	}
	if fields.SelfName != nil {
		steps[1].value = *fields.SelfName
	}
	if fields.SelfURL != nil {
		steps[2].value = *fields.SelfURL
	}
	if fields.Share != nil {
		encoded, err := json.Marshal(fields.Share)
		if err != nil {
			return fmt.Errorf("pools: encode the share: %w", err)
		}
		steps[3].value = string(encoded)
	}
	if fields.LastSyncAt != nil {
		steps[4].value = *fields.LastSyncAt
	}
	if fields.Paused != nil {
		steps[5].value = boolInt(*fields.Paused)
	}
	if fields.RelayEnabled != nil {
		steps[6].value = boolInt(*fields.RelayEnabled)
	}
	if fields.HostRelay != nil {
		steps[7].value = boolInt(*fields.HostRelay)
	}
	for _, step := range steps {
		if !step.ok {
			continue
		}
		if _, err := s.DB.W.ExecContext(ctx,
			`UPDATE pools SET `+step.column+` = ? WHERE user_id = ? AND pool_id = ?`,
			step.value, userID, poolID); err != nil {
			return fmt.Errorf("pools: update pool: %w", err)
		}
	}
	return nil
}

// deletePool removes everything this person has about one pool.
func (s *Service) deletePool(ctx context.Context, userID, poolID string) error {
	for _, table := range []string{"pools", "pool_members", "pool_invites", "pool_shared_accounts"} {
		if _, err := s.DB.W.ExecContext(ctx,
			`DELETE FROM `+table+` WHERE user_id = ? AND pool_id = ?`, userID, poolID); err != nil {
			return fmt.Errorf("pools: delete pool rows: %w", err)
		}
	}
	return nil
}

// member is one partner in one pool.
type member struct {
	MemberID  string
	Name      string
	URL       string
	PublicKey string
	JoinedAt  string
	BoxKey    string
	BoxSig    string
	Features  []string
}

const memberColumns = `member_id, name, url, public_key, joined_at, box_key, box_sig, features_json`

func scanMember(scan func(...any) error) (*member, error) {
	var (
		row      member
		boxKey   *string
		boxSig   *string
		features string
	)
	if err := scan(&row.MemberID, &row.Name, &row.URL, &row.PublicKey, &row.JoinedAt, &boxKey, &boxSig, &features); err != nil {
		return nil, err
	}
	if boxKey != nil {
		row.BoxKey = *boxKey
	}
	if boxSig != nil {
		row.BoxSig = *boxSig
	}
	row.Features = []string{}
	if features != "" {
		_ = json.Unmarshal([]byte(features), &row.Features)
	}
	if row.Features == nil {
		row.Features = []string{}
	}
	return &row, nil
}

// members lists one pool's members as this person sees them.
func (s *Service) members(ctx context.Context, userID, poolID string) ([]*member, error) {
	rows, err := s.DB.R.QueryContext(ctx, `SELECT `+memberColumns+
		` FROM pool_members WHERE user_id = ? AND pool_id = ? ORDER BY joined_at, rowid`, userID, poolID)
	if err != nil {
		return nil, fmt.Errorf("pools: list pool members: %w", err)
	}
	defer rows.Close()
	list := []*member{}
	for rows.Next() {
		row, err := scanMember(rows.Scan)
		if err != nil {
			return nil, fmt.Errorf("pools: read pool member: %w", err)
		}
		list = append(list, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("pools: list pool members: %w", err)
	}
	return list, nil
}

// member reads one member of one pool, nil when unknown.
func (s *Service) member(ctx context.Context, userID, poolID, memberID string) (*member, error) {
	row := s.DB.R.QueryRowContext(ctx, `SELECT `+memberColumns+
		` FROM pool_members WHERE user_id = ? AND pool_id = ? AND member_id = ?`, userID, poolID, memberID)
	found, err := scanMember(row.Scan)
	if err != nil {
		if isNoRows(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("pools: read pool member: %w", err)
	}
	return found, nil
}

// upsertMember stores one member. The public key is fixed at first sight; the
// sealing key only comes with a valid signature (checked by the caller) and is
// kept when an update has none.
func (s *Service) upsertMember(ctx context.Context, userID, poolID string, value member) error {
	features := value.Features
	if features == nil {
		features = []string{}
	}
	encoded, err := json.Marshal(features)
	if err != nil {
		return fmt.Errorf("pools: encode member features: %w", err)
	}
	var boxKey, boxSig any
	if value.BoxKey != "" {
		boxKey = value.BoxKey
	}
	if value.BoxSig != "" {
		boxSig = value.BoxSig
	}
	_, err = s.DB.W.ExecContext(ctx, `INSERT INTO pool_members (user_id, pool_id, member_id, name, url,
		public_key, box_key, box_sig, features_json) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (user_id, pool_id, member_id) DO UPDATE SET name = excluded.name, url = excluded.url,
			features_json = excluded.features_json, box_key = COALESCE(excluded.box_key, box_key),
			box_sig = COALESCE(excluded.box_sig, box_sig)`,
		userID, poolID, value.MemberID, value.Name, value.URL, value.PublicKey, boxKey, boxSig, string(encoded))
	if err != nil {
		return fmt.Errorf("pools: store pool member: %w", err)
	}
	return nil
}

func (s *Service) removeMemberRow(ctx context.Context, userID, poolID, memberID string) error {
	if _, err := s.DB.W.ExecContext(ctx,
		`DELETE FROM pool_members WHERE user_id = ? AND pool_id = ? AND member_id = ?`,
		userID, poolID, memberID); err != nil {
		return fmt.Errorf("pools: remove pool member: %w", err)
	}
	return nil
}

// peerLink is one partner link in one pool.
type peerLink struct {
	ID          string
	UserID      string
	PoolID      string
	MemberID    string
	Name        string
	URL         string
	PublicKey   string
	AccountID   *string
	RouterKeyID *string
	Status      string
	LastError   *string
	CreatedAt   string
}

const linkColumns = `id, user_id, pool_id, member_id, name, url, public_key, account_id, router_key_id,
	status, last_error, created_at`

func scanLink(scan func(...any) error) (*peerLink, error) {
	var row peerLink
	if err := scan(&row.ID, &row.UserID, &row.PoolID, &row.MemberID, &row.Name, &row.URL, &row.PublicKey,
		&row.AccountID, &row.RouterKeyID, &row.Status, &row.LastError, &row.CreatedAt); err != nil {
		if isNoRows(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("pools: read partner link: %w", err)
	}
	return &row, nil
}

func (s *Service) links(ctx context.Context, userID, poolID string) ([]*peerLink, error) {
	rows, err := s.DB.R.QueryContext(ctx, `SELECT `+linkColumns+
		` FROM peer_links WHERE user_id = ? AND pool_id = ? ORDER BY created_at`, userID, poolID)
	if err != nil {
		return nil, fmt.Errorf("pools: list partner links: %w", err)
	}
	defer rows.Close()
	list := []*peerLink{}
	for rows.Next() {
		row, err := scanLink(rows.Scan)
		if err != nil {
			return nil, err
		}
		if row != nil {
			list = append(list, row)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("pools: list partner links: %w", err)
	}
	return list, nil
}

func (s *Service) link(ctx context.Context, userID, linkID string) (*peerLink, error) {
	row := s.DB.R.QueryRowContext(ctx, `SELECT `+linkColumns+` FROM peer_links WHERE user_id = ? AND id = ?`,
		userID, linkID)
	return scanLink(row.Scan)
}

func (s *Service) linkByID(ctx context.Context, linkID string) (*peerLink, error) {
	row := s.DB.R.QueryRowContext(ctx, `SELECT `+linkColumns+` FROM peer_links WHERE id = ?`, linkID)
	return scanLink(row.Scan)
}

func (s *Service) linkToMember(ctx context.Context, userID, poolID, memberID string) (*peerLink, error) {
	row := s.DB.R.QueryRowContext(ctx, `SELECT `+linkColumns+
		` FROM peer_links WHERE user_id = ? AND pool_id = ? AND member_id = ?`, userID, poolID, memberID)
	return scanLink(row.Scan)
}

func (s *Service) linkForAccount(ctx context.Context, accountID string) (*peerLink, error) {
	row := s.DB.R.QueryRowContext(ctx, `SELECT `+linkColumns+` FROM peer_links WHERE account_id = ?`, accountID)
	return scanLink(row.Scan)
}

// insertLink adds a partner link and returns it.
func (s *Service) insertLink(ctx context.Context, userID, poolID string, value member) (*peerLink, error) {
	id := uuid.NewString()
	if _, err := s.DB.W.ExecContext(ctx, `INSERT INTO peer_links (id, user_id, pool_id, member_id, name, url,
		public_key) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		id, userID, poolID, value.MemberID, value.Name, value.URL, value.PublicKey); err != nil {
		return nil, fmt.Errorf("pools: insert partner link: %w", err)
	}
	return s.link(ctx, userID, id)
}

// linkUpdate is the set of link columns a change may touch.
type linkUpdate struct {
	AccountID   *string
	RouterKeyID *string
	Status      *string
	LastError   *string
	Name        *string
	URL         *string
	ClearError  bool
}

func (s *Service) updateLink(ctx context.Context, linkID string, fields linkUpdate) error {
	type step struct {
		column string
		value  any
		ok     bool
	}
	steps := []step{
		{"account_id", nil, fields.AccountID != nil},
		{"router_key_id", nil, fields.RouterKeyID != nil},
		{"status", nil, fields.Status != nil},
		{"last_error", nil, fields.ClearError},
		{"name", nil, fields.Name != nil},
		{"url", nil, fields.URL != nil},
	}
	if fields.AccountID != nil {
		steps[0].value = *fields.AccountID
	}
	if fields.RouterKeyID != nil {
		steps[1].value = *fields.RouterKeyID
	}
	if fields.Status != nil {
		steps[2].value = *fields.Status
	}
	if fields.LastError != nil {
		steps[3].value = *fields.LastError
	}
	if fields.Name != nil {
		steps[4].value = *fields.Name
	}
	if fields.URL != nil {
		steps[5].value = *fields.URL
	}
	for _, item := range steps {
		if !item.ok {
			continue
		}
		if _, err := s.DB.W.ExecContext(ctx, `UPDATE peer_links SET `+item.column+` = ? WHERE id = ?`,
			item.value, linkID); err != nil {
			return fmt.Errorf("pools: update partner link: %w", err)
		}
	}
	return nil
}

func (s *Service) deleteLink(ctx context.Context, linkID string) error {
	if _, err := s.DB.W.ExecContext(ctx, `DELETE FROM peer_links WHERE id = ?`, linkID); err != nil {
		return fmt.Errorf("pools: delete partner link: %w", err)
	}
	return nil
}

// invite is one open invite link of a pool.
type invite struct {
	ID        string
	UserID    string
	PoolID    string
	TokenHash string
	ExpiresAt int64
	UsedAt    *string
	UsedBy    *string
	RevokedAt *string
	CreatedAt string
}

func (s *Service) invites(ctx context.Context, userID, poolID string) ([]invite, error) {
	rows, err := s.DB.R.QueryContext(ctx, `SELECT id, user_id, pool_id, token_hash, expires_at, used_at, used_by,
		revoked_at, created_at FROM pool_invites WHERE user_id = ? AND pool_id = ? AND used_at IS NULL
		AND revoked_at IS NULL AND expires_at > ? ORDER BY created_at DESC`, userID, poolID, s.nowMS())
	if err != nil {
		return nil, fmt.Errorf("pools: list invites: %w", err)
	}
	defer rows.Close()
	list := []invite{}
	for rows.Next() {
		var row invite
		if err := rows.Scan(&row.ID, &row.UserID, &row.PoolID, &row.TokenHash, &row.ExpiresAt, &row.UsedAt,
			&row.UsedBy, &row.RevokedAt, &row.CreatedAt); err != nil {
			return nil, fmt.Errorf("pools: read invite: %w", err)
		}
		list = append(list, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("pools: list invites: %w", err)
	}
	return list, nil
}

func (s *Service) insertInvite(ctx context.Context, userID, poolID, tokenHash string, expiresAt int64) (string, error) {
	id := uuid.NewString()
	if _, err := s.DB.W.ExecContext(ctx,
		`INSERT INTO pool_invites (id, user_id, pool_id, token_hash, expires_at) VALUES (?, ?, ?, ?, ?)`,
		id, userID, poolID, tokenHash, expiresAt); err != nil {
		return "", fmt.Errorf("pools: insert invite: %w", err)
	}
	return id, nil
}

func (s *Service) inviteByHash(ctx context.Context, tokenHash string) (*invite, error) {
	var row invite
	err := s.DB.R.QueryRowContext(ctx, `SELECT id, user_id, pool_id, token_hash, expires_at, used_at, used_by,
		revoked_at, created_at FROM pool_invites WHERE token_hash = ?`, tokenHash).
		Scan(&row.ID, &row.UserID, &row.PoolID, &row.TokenHash, &row.ExpiresAt, &row.UsedAt, &row.UsedBy,
			&row.RevokedAt, &row.CreatedAt)
	if err != nil {
		if isNoRows(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("pools: read invite: %w", err)
	}
	return &row, nil
}

// useInvite marks the invite used, once: false when someone else used it first.
func (s *Service) useInvite(ctx context.Context, inviteID, memberID string) (bool, error) {
	result, err := s.DB.W.ExecContext(ctx, `UPDATE pool_invites SET used_at =
		strftime('%Y-%m-%dT%H:%M:%fZ','now'), used_by = ? WHERE id = ? AND used_at IS NULL`, memberID, inviteID)
	if err != nil {
		return false, fmt.Errorf("pools: use invite: %w", err)
	}
	changes, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("pools: use invite: %w", err)
	}
	return changes == 1, nil
}

func (s *Service) revokeInviteRow(ctx context.Context, userID, poolID, inviteID string) (bool, error) {
	result, err := s.DB.W.ExecContext(ctx, `UPDATE pool_invites SET revoked_at = strftime('%Y-%m-%dT%H:%M:%fZ','now')
		WHERE id = ? AND user_id = ? AND pool_id = ? AND revoked_at IS NULL`, inviteID, userID, poolID)
	if err != nil {
		return false, fmt.Errorf("pools: revoke invite: %w", err)
	}
	changes, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("pools: revoke invite: %w", err)
	}
	return changes == 1, nil
}

func (s *Service) activeInviteCount(ctx context.Context, userID, poolID string) (int, error) {
	var count int
	err := s.DB.R.QueryRowContext(ctx, `SELECT COUNT(*) FROM pool_invites WHERE user_id = ? AND pool_id = ?
		AND used_at IS NULL AND revoked_at IS NULL AND expires_at > ?`, userID, poolID, s.nowMS()).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("pools: count invites: %w", err)
	}
	return count, nil
}

// shareKinds are the kinds of model a shared connection gives a pool.
var shareKinds = []string{"free", "plan", "paid"}

// shareColumns maps a kind to its column.
var shareColumns = map[string]string{"free": "share_free", "plan": "share_plan", "paid": "share_paid"}

// kindsOf are the kinds one pool_shared_accounts row shares.
func kindsOf(row map[string]int) []string {
	list := []string{}
	for _, kind := range shareKinds {
		if row[kind] == 1 {
			list = append(list, kind)
		}
	}
	return list
}

func (s *Service) sharedAccountIDs(ctx context.Context, userID, poolID string) ([]string, error) {
	rows, err := s.DB.R.QueryContext(ctx,
		`SELECT account_id FROM pool_shared_accounts WHERE user_id = ? AND pool_id = ?`, userID, poolID)
	if err != nil {
		return nil, fmt.Errorf("pools: list shared connections: %w", err)
	}
	defer rows.Close()
	list := []string{}
	for rows.Next() {
		var accountID string
		if err := rows.Scan(&accountID); err != nil {
			return nil, fmt.Errorf("pools: read shared connection: %w", err)
		}
		list = append(list, accountID)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("pools: list shared connections: %w", err)
	}
	return list, nil
}

// shareKindsByAccount is what each shared connection shares in one pool.
func (s *Service) shareKindsByAccount(ctx context.Context, userID, poolID string) (map[string][]string, error) {
	rows, err := s.DB.R.QueryContext(ctx, `SELECT account_id, share_free, share_plan, share_paid
		FROM pool_shared_accounts WHERE user_id = ? AND pool_id = ?`, userID, poolID)
	if err != nil {
		return nil, fmt.Errorf("pools: list shared kinds: %w", err)
	}
	defer rows.Close()
	result := map[string][]string{}
	for rows.Next() {
		var accountID string
		counts := map[string]int{}
		var free, plan, paid int
		if err := rows.Scan(&accountID, &free, &plan, &paid); err != nil {
			return nil, fmt.Errorf("pools: read shared kinds: %w", err)
		}
		counts["free"], counts["plan"], counts["paid"] = free, plan, paid
		result[accountID] = kindsOf(counts)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("pools: list shared kinds: %w", err)
	}
	return result, nil
}

// shareChange is what one connection's share becomes: shared false removes it,
// kinds picks which kinds it shares (nil keeps them), no kinds removes it.
type shareChange struct {
	Shared bool
	Kinds  []string
	// SharedSet distinguishes "shared given" from "kinds only" (the default is
	// to share).
	SharedSet bool
	// KindsSet distinguishes "no kinds given" from "the empty list".
	KindsSet bool
}

// setShare writes one connection's share in one pool.
func (s *Service) setShare(ctx context.Context, userID, poolID, accountID string, change shareChange) error {
	where := `WHERE user_id = ? AND pool_id = ? AND account_id = ?`
	if !change.Shared || (change.KindsSet && len(change.Kinds) == 0) {
		if _, err := s.DB.W.ExecContext(ctx, `DELETE FROM pool_shared_accounts `+where,
			userID, poolID, accountID); err != nil {
			return fmt.Errorf("pools: unshare connection: %w", err)
		}
		return nil
	}
	if _, err := s.DB.W.ExecContext(ctx,
		`INSERT OR IGNORE INTO pool_shared_accounts (user_id, pool_id, account_id) VALUES (?, ?, ?)`,
		userID, poolID, accountID); err != nil {
		return fmt.Errorf("pools: share connection: %w", err)
	}
	if !change.KindsSet {
		return nil
	}
	set := make([]string, 0, len(shareKinds))
	for _, kind := range shareKinds {
		value := 0
		if containsString(change.Kinds, kind) {
			value = 1
		}
		set = append(set, fmt.Sprintf("%s = %d", shareColumns[kind], value))
	}
	if _, err := s.DB.W.ExecContext(ctx, `UPDATE pool_shared_accounts SET `+strings.Join(set, ", ")+` `+where,
		userID, poolID, accountID); err != nil {
		return fmt.Errorf("pools: set share kinds: %w", err)
	}
	return nil
}

// shareKindsOfRow is the kinds a connection shares in a pool, nil when it is not
// shared there.
func (s *Service) shareKindsOfRow(ctx context.Context, poolID, accountID string) ([]string, error) {
	var free, plan, paid int
	err := s.DB.R.QueryRowContext(ctx, `SELECT share_free, share_plan, share_paid FROM pool_shared_accounts
		WHERE pool_id = ? AND account_id = ?`, poolID, accountID).Scan(&free, &plan, &paid)
	if err != nil {
		if isNoRows(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("pools: read shared kinds: %w", err)
	}
	return kindsOf(map[string]int{"free": free, "plan": plan, "paid": paid}), nil
}

// poolMembership is one (person, pool) pair.
type poolMembership struct {
	UserID string
	PoolID string
}

func (s *Service) poolMemberships(ctx context.Context) []poolMembership {
	rows, err := s.DB.R.QueryContext(ctx, `SELECT user_id, pool_id FROM pools`)
	if err != nil {
		s.log().Warn("Could not list pool memberships", "error", err)
		return nil
	}
	defer rows.Close()
	list := []poolMembership{}
	for rows.Next() {
		var row poolMembership
		if err := rows.Scan(&row.UserID, &row.PoolID); err != nil {
			s.log().Warn("Could not read pool membership", "error", err)
			return list
		}
		list = append(list, row)
	}
	return list
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

// dayOf is the UTC day a timestamp belongs to.
func dayOf(ms int64) string { return time.UnixMilli(ms).UTC().Format("2006-01-02") }
