package pools

// Pools: people on separate NeoRouters who share their free capacity
// (pool_service.js). A person may be in several pools; each has its own partners,
// sharing, caps and pause switch.
//
// Control goes through the host (who created the pool): it keeps the member list
// and the invites. Every two members of a pool are linked (links.go): directly
// when both have a public address, and through the host when one of them has none
// (relay_*.go: sealed end to end, so the host only passes it on). Relaying is an
// optional extra on protocol v2: NeoRouters without it ignore the new fields and
// leave members that have only a relay address out.
//
// Joining with an invite link:
//  1. the joiner's server asks the host for a preview (pool, members)
//  2. it sends a signed "join" with the invite token and a key for the host; the
//     host answers with a key for the joiner, the member list and one signed
//     introduction ticket per other member
//  3. with each ticket the joiner introduces itself to that member, who checks the
//     host's signature and exchanges keys the same way
//
// Later changes (someone left, was removed, joined while a member was offline) are
// picked up by sync, which compares with the host's signed member list. Every
// message names its pool.

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// lookUpGap is how long a partner's sealing key is looked for at most once.
const lookUpGap = 60 * time.Second

// presenceGap is how often a partner's connection is re-checked when it comes
// online.
const presenceGap = 30 * time.Second

// presenceEvent is one partner coming online or going away.
type presenceEvent struct {
	UserID   string
	PoolID   string
	MemberID string
	Online   bool
}

// relayed reports whether the relay is offered by this pool's host (seen from
// this person's side).
func relayed(owner *pool) bool {
	if owner == nil {
		return false
	}
	if owner.IsHost {
		return owner.RelayEnabled
	}
	return owner.HostRelay
}

// endpointFor is how this person asks partners through the host in this pool, nil
// when the relay is not available.
func (s *Service) endpointFor(ctx context.Context, userID, poolID string) *relayEndpoint {
	owner, err := s.pool(ctx, userID, poolID)
	if err != nil || !relayed(owner) {
		return nil
	}
	if owner.IsHost {
		endpoint, err := s.hub.localEndpoint(ctx, userID, poolID)
		if err != nil {
			s.log().Warn("Could not open the host's own relay end", "error", err)
			return nil
		}
		return endpoint
	}
	return s.connectorEndpointFor(userID, poolID)
}

// canUse reports whether a relay address can be used by this person at all (they
// are in its pool, and it relays).
func (s *Service) canUse(ctx context.Context, userID, value string) bool {
	target, ok := parseRelayAddress(value)
	if !ok {
		return false
	}
	owner, err := s.pool(ctx, userID, target.PoolID)
	if err != nil {
		return false
	}
	return relayed(owner)
}

// relayOnline reports true/false when known, and false when we cannot tell (not
// connected to the host).
func (s *Service) relayOnline(ctx context.Context, userID, poolID, memberID string) (bool, bool) {
	owner, err := s.pool(ctx, userID, poolID)
	if err != nil || !relayed(owner) {
		return false, false
	}
	if owner.IsHost {
		return s.hub.isOnline(poolID, memberID), true
	}
	return s.connectorIsOnline(userID, poolID, memberID)
}

// lookUpSender looks a partner we have no sealing key for up in the host's list:
// at most once a minute per partner, and concurrent lookups share one sync.
func (s *Service) lookUpSender(ctx context.Context, userID, poolID, memberID string) error {
	key := userID + "|" + poolID + "|" + memberID
	poolKey := userID + "|" + poolID
	s.lookupMu.Lock()
	if last, ok := s.lookups[key]; ok && s.nowMS()-last.at < lookUpGap.Milliseconds() {
		s.lookupMu.Unlock()
		return last.wait(ctx)
	}
	done, running := s.syncs[poolKey]
	if !running {
		done = make(chan struct{})
		s.syncs[poolKey] = done
		go func() {
			defer func() {
				s.lookupMu.Lock()
				delete(s.syncs, poolKey)
				s.lookupMu.Unlock()
				close(done)
			}()
			if err := s.sync(context.Background(), userID, poolID); err != nil {
				s.log().Warn("Pool sync while looking a partner up failed", "error", err)
			}
		}()
	}
	if len(s.lookups) > 5000 {
		s.lookups = map[string]lookupEntry{}
	}
	entry := lookupEntry{at: s.nowMS(), done: done}
	s.lookups[key] = entry
	s.lookupMu.Unlock()
	return entry.wait(ctx)
}

// lookupEntry is one running look-up.
type lookupEntry struct {
	at   int64
	done chan struct{}
}

// wait blocks until the look-up finished.
func (l lookupEntry) wait(ctx context.Context) error {
	if ctx == nil {
		<-l.done
		return nil
	}
	select {
	case <-l.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// refreshPartner re-fetches a partner's models and forgets what went wrong before
// (a partner is back or shares something new).
func (s *Service) refreshPartner(ctx context.Context, link *peerLink, recheck bool) {
	if link == nil || link.AccountID == nil {
		return
	}
	accountID := *link.AccountID
	fail := func(err error) {
		message := err.Error()
		if updateErr := s.updateLink(ctx, link.ID, linkUpdate{LastError: &message}); updateErr != nil {
			s.log().Warn("Could not record a partner refresh failure", "linkId", link.ID, "error", updateErr)
		}
	}
	// A partner that could not be reached when we linked is checked again.
	if recheck && s.Accounts != nil {
		if err := s.Accounts.Validate(ctx, accountID); err != nil {
			fail(err)
			return
		}
	}
	if s.Catalog != nil {
		if err := s.Catalog(ctx, accountID); err != nil {
			fail(err)
			return
		}
	}
	if s.MarkFixed != nil {
		s.MarkFixed(accountID, s.now())
	}
	if s.ForgetQuota != nil {
		refs := []int64{}
		rows, err := s.DB.R.QueryContext(ctx, `SELECT id FROM models WHERE account_id = ?`, accountID)
		if err == nil {
			for rows.Next() {
				var ref int64
				if scanErr := rows.Scan(&ref); scanErr == nil {
					refs = append(refs, ref)
				}
			}
			rows.Close()
		}
		if err := s.ForgetQuota(accountID, refs); err != nil {
			s.log().Warn("Could not forget what was observed about a partner", "error", err)
		}
	}
	if err := s.updateLink(ctx, link.ID, linkUpdate{ClearError: true}); err != nil {
		s.log().Warn("Could not clear a partner link error", "linkId", link.ID, "error", err)
	}
}

// onPresence handles a relayed member coming online: what failed while it was away
// is forgotten at once, and its connection is checked again and its models fetched
// (that part at most every half minute).
func (s *Service) onPresence(event presenceEvent) {
	if !event.Online {
		return
	}
	owner := event.UserID
	if owner == "" {
		hosted, err := s.hostedPool(context.Background(), event.PoolID)
		if err == nil && hosted != nil {
			owner = hosted.UserID
		}
	}
	if owner == "" {
		return
	}
	link, err := s.linkToMember(context.Background(), owner, event.PoolID, event.MemberID)
	if err != nil || link == nil || link.AccountID == nil || !isRelayAddress(link.URL) {
		return
	}
	if s.MarkFixed != nil {
		s.MarkFixed(*link.AccountID, s.now())
	}
	now := s.nowMS()
	s.lookupMu.Lock()
	last := s.refreshedAt[link.ID]
	if now-last < presenceGap.Milliseconds() {
		s.lookupMu.Unlock()
		return
	}
	if len(s.refreshedAt) > 5000 {
		s.refreshedAt = map[string]int64{}
	}
	s.refreshedAt[link.ID] = now
	s.lookupMu.Unlock()
	go s.refreshPartner(context.Background(), link, true)
}

// assertPeerKey checks the shape of a partner's router key.
func assertPeerKey(value string) error {
	if !peerKeyText.MatchString(value) {
		return fail(http.StatusBadRequest, "POOL_MESSAGE_INVALID", "The partner key is malformed.")
	}
	return nil
}

// sanitizeMember checks a member as sent by another server, field by field. A
// relay address must be this member's own in this pool, and a sealing key counts
// only with a valid signature by the member's pool key.
func sanitizeMember(raw map[string]any, poolID string) *member {
	if raw == nil {
		return nil
	}
	memberID := strings.ToLower(stringOf(raw["memberId"]))
	publicKey := stringOf(raw["publicKey"])
	url := originOf(stringOf(raw["url"]))
	if !memberIDText.MatchString(memberID) || !publicKeyText.MatchString(publicKey) || url == "" {
		return nil
	}
	if relayed, ok := parseRelayAddress(url); ok {
		if relayed.MemberID != memberID || relayed.PoolID != strings.ToLower(poolID) {
			return nil
		}
	}
	result := &member{
		MemberID:  memberID,
		Name:      cleanName(stringOf(raw["name"]), "Partner"),
		URL:       url,
		PublicKey: publicKey,
		Features:  []string{},
	}
	if joined, ok := raw["joinedAt"].(string); ok {
		if len(joined) > 40 {
			joined = truncateUTF16(joined, 40)
		}
		result.JoinedAt = joined
	}
	boxKey := stringOf(raw["boxKey"])
	boxSig := stringOf(raw["boxSig"])
	if verifyBoxKey(publicKey, boxKey, boxSig) {
		result.BoxKey = boxKey
		result.BoxSig = boxSig
	}
	if features, ok := raw["features"].([]any); ok {
		for _, feature := range poolFeatures {
			for _, offered := range features {
				if text, _ := offered.(string); text == feature {
					result.Features = append(result.Features, feature)
				}
			}
		}
	}
	return result
}

// rememberMember stores a member another server told us about. A member's pool key
// is fixed at first sight, and its sealing key is only taken when signed by that
// same key: a later list cannot swap the sealing key of a member we already know.
func (s *Service) rememberMember(ctx context.Context, userID, poolID string, value *member) error {
	known, err := s.member(ctx, userID, poolID, value.MemberID)
	if err != nil {
		return err
	}
	stored := *value
	if known != nil && known.PublicKey != value.PublicKey {
		stored.PublicKey = known.PublicKey
		stored.BoxKey = ""
		stored.BoxSig = ""
	}
	return s.upsertMember(ctx, userID, poolID, stored)
}

// publicMember is one member as it is sent to partners.
type publicMember struct {
	MemberID  string   `json:"memberId"`
	Name      string   `json:"name"`
	URL       string   `json:"url"`
	PublicKey string   `json:"publicKey"`
	JoinedAt  *string  `json:"joinedAt"`
	BoxKey    string   `json:"boxKey,omitempty"`
	BoxSig    string   `json:"boxSig,omitempty"`
	Features  []string `json:"features,omitempty"`
}

// publicMemberOf is one stored member as partners receive it.
func publicMemberOf(value *member) publicMember {
	public := publicMember{
		MemberID: value.MemberID, Name: value.Name, URL: value.URL, PublicKey: value.PublicKey,
	}
	if value.JoinedAt != "" {
		joined := value.JoinedAt
		public.JoinedAt = &joined
	}
	if value.BoxKey != "" {
		public.BoxKey = value.BoxKey
		public.BoxSig = value.BoxSig
	}
	if len(value.Features) > 0 {
		public.Features = value.Features
	}
	return public
}

// selfMember is this person's own entry in a pool.
func (s *Service) selfMember(ctx context.Context, userID string, owner *pool) (*member, error) {
	record, err := s.ensureIdentity(ctx, userID)
	if err != nil {
		return nil, err
	}
	boxKey, boxSig, err := s.boxCard(ctx, userID)
	if err != nil {
		return nil, err
	}
	return &member{
		MemberID: record.MemberID, Name: owner.SelfName, URL: owner.SelfURL, PublicKey: record.PublicKey,
		BoxKey: boxKey, BoxSig: boxSig, Features: append([]string(nil), poolFeatures...),
	}, nil
}

// hostFeatures is what a host tells members about itself.
func hostFeatures(owner *pool) []string {
	if owner.RelayEnabled {
		return []string{"relay"}
	}
	return []string{}
}

// offersRelay reports whether a feature list offers relaying.
func offersRelay(features []any) bool {
	for _, feature := range features {
		if text, _ := feature.(string); text == "relay" {
			return true
		}
	}
	return false
}

// setSharedAccounts shares exactly these of the person's own connections in this
// pool; kinds sets what some of them share (the others keep theirs, new ones share
// free models).
func (s *Service) setSharedAccounts(ctx context.Context, userID, poolID string, accountIDs []string, kinds map[string][]string) error {
	wanted := map[string]bool{}
	for _, id := range accountIDs {
		wanted[id] = true
	}
	rows, err := s.DB.R.QueryContext(ctx, `SELECT id FROM provider_accounts WHERE user_id = ?
		AND definition_id != ?`, userID, PeerDefinition)
	if err != nil {
		return fmt.Errorf("pools: list own connections: %w", err)
	}
	ids := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return fmt.Errorf("pools: read own connection: %w", err)
		}
		ids = append(ids, id)
	}
	rows.Close()
	for _, id := range ids {
		change := shareChange{Shared: wanted[id]}
		if sharedKinds, given := kinds[id]; given {
			change.Kinds = sharedKinds
			change.KindsSet = true
		}
		if _, err := s.SetAccountShare(ctx, userID, poolID, id, change); err != nil {
			return err
		}
	}
	return nil
}

// ShareableAccount is one of a person's own connections that could be shared.
type ShareableAccount struct {
	ID           string `json:"id"`
	Label        string `json:"label"`
	DefinitionID string `json:"definitionId"`
	Enabled      bool   `json:"enabled"`
	HasBalance   bool   `json:"hasBalance"`
	FreeModels   int64  `json:"freeModels"`
	PlanModels   int64  `json:"planModels"`
	PaidModels   int64  `json:"paidModels"`
}

// shareableAccounts lists own connections with chat models that could be shared,
// and how many of each kind: free, plan (subscription) and paid. Plan and paid
// models are only shared where the owner ticks them. hasBalance: the provider
// reports a balance, so paid partner use spends it; otherwise it is billed.
func (s *Service) shareableAccounts(ctx context.Context, userID string) ([]ShareableAccount, error) {
	rows, err := s.DB.R.QueryContext(ctx, `SELECT a.id, a.label, a.definition_id, a.enabled, a.prepaid,
		(SELECT COUNT(*) FROM models m WHERE m.account_id = a.id AND m.is_free = 1 AND m.available = 1
			AND m.capability = 'chat') free_models,
		(SELECT COUNT(*) FROM models m WHERE m.account_id = a.id AND m.is_free = 0 AND m.available = 1
			AND m.capability = 'chat') other_models,
		EXISTS (SELECT 1 FROM quota_state q WHERE q.scope_key = 'a:' || a.id AND q.metric = 'balance') has_balance
		FROM provider_accounts a WHERE a.user_id = ? AND a.definition_id != ? ORDER BY a.label COLLATE NOCASE`,
		userID, PeerDefinition)
	if err != nil {
		return nil, fmt.Errorf("pools: list shareable connections: %w", err)
	}
	defer rows.Close()
	list := []ShareableAccount{}
	for rows.Next() {
		var (
			account     ShareableAccount
			enabled     int
			prepaid     int
			free, other int64
			hasBalance  int
		)
		if err := rows.Scan(&account.ID, &account.Label, &account.DefinitionID, &enabled, &prepaid,
			&free, &other, &hasBalance); err != nil {
			return nil, fmt.Errorf("pools: read shareable connection: %w", err)
		}
		if free == 0 && other == 0 {
			continue
		}
		account.Enabled = enabled == 1
		account.HasBalance = hasBalance == 1
		account.FreeModels = free
		if prepaid == 1 {
			account.PlanModels = other
		} else {
			account.PaidModels = other
		}
		list = append(list, account)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("pools: list shareable connections: %w", err)
	}
	return list, nil
}

// randomToken is a new invite or peer token: n random bytes, URL-safe.
func randomToken(bytes int) (string, error) {
	raw := make([]byte, bytes)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("pools: make a token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

// hashToken is the stored digest of an invite token.
func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// stringOf reads one field of a decoded JSON object as text.
func stringOf(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case nil:
		return ""
	default:
		encoded, err := json.Marshal(typed)
		if err != nil {
			return ""
		}
		return string(encoded)
	}
}
