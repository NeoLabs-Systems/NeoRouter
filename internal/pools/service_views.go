package pools

// The pool page's view of a person's pools, and the switches and messages that
// change them.

import (
	"context"
	"fmt"
	"net/http"
)

// Summary is the whole pool page state.
type Summary struct {
	Pools     []poolView         `json:"pools"`
	Privacy   PoolPrivacy        `json:"privacy"`
	Shareable []ShareableAccount `json:"shareable"`
	Defaults  ShareDefaults      `json:"defaults"`
	MaxPools  int                `json:"maxPools"`
}

// ShareDefaults is the automatic share and how the balance scales it.
type ShareDefaults struct {
	RequestsPerDay jsNum              `json:"requestsPerDay"`
	TokensPerDay   jsNum              `json:"tokensPerDay"`
	Concurrent     jsNum              `json:"concurrent"`
	Reserve        *jsNum             `json:"reserve"`
	Balance        ShareBalanceLimits `json:"balance"`
}

// ShareBalanceLimits is how far the balance may scale the automatic share.
type ShareBalanceLimits struct {
	Floor   float64 `json:"floor"`
	Ceiling float64 `json:"ceiling"`
}

// createResult is a new (or joined) pool plus the page state.
type createResult struct {
	PoolID string `json:"poolId"`
	Summary
}

// poolView is one pool as the page shows it.
type poolView struct {
	ID               string              `json:"id"`
	Name             string              `json:"name"`
	IsHost           bool                `json:"isHost"`
	Paused           bool                `json:"paused"`
	SelfName         string              `json:"selfName"`
	Relay            poolRelayView       `json:"relay"`
	JoinedAt         string              `json:"joinedAt"`
	LastSyncAt       *string             `json:"lastSyncAt"`
	Partners         []partnerView       `json:"partners"`
	Share            ShareSettings       `json:"share"`
	SharedAccountIDs []string            `json:"sharedAccountIds"`
	ShareKinds       map[string][]string `json:"shareKinds"`
	Invites          []inviteView        `json:"invites"`
}

// poolRelayView is how a pool relays, from this person's side.
type poolRelayView struct {
	Offered   bool  `json:"offered"`
	ViaHost   bool  `json:"viaHost"`
	Connected *bool `json:"connected"`
}

// inviteView is one open invite link.
type inviteView struct {
	ID        string `json:"id"`
	ExpiresAt string `json:"expiresAt"`
	CreatedAt string `json:"createdAt"`
}

// partnerView is one partner as the page shows it.
type partnerView struct {
	MemberID  string          `json:"memberId"`
	Name      string          `json:"name"`
	IsHost    bool            `json:"isHost"`
	ViaHost   bool            `json:"viaHost"`
	RelayDown bool            `json:"relayDown"`
	LinkID    *string         `json:"linkId"`
	State     string          `json:"state"`
	Problem   *string         `json:"problem"`
	Models    int64           `json:"models"`
	Balance   *partnerBalance `json:"balance"`
	Today     *partnerToday   `json:"today"`
	Caps      *partnerCaps    `json:"caps"`
}

// partnerBalance is what one partner gives and takes, and how that scales the
// share.
type partnerBalance struct {
	State  string      `json:"state"`
	Factor float64     `json:"factor"`
	Week   balanceWeek `json:"week"`
}

type balanceWeek struct {
	Given balanceSide `json:"given"`
	Taken balanceSide `json:"taken"`
}

// partnerToday is today's counts with one partner.
type partnerToday struct {
	Given balanceSide `json:"given"`
	Taken balanceSide `json:"taken"`
}

// partnerCaps is what one partner may use of ours today.
type partnerCaps struct {
	RequestsPerDay jsNum `json:"requestsPerDay"`
	TokensPerDay   jsNum `json:"tokensPerDay"`
	Concurrent     jsNum `json:"concurrent"`
}

// partnerViewOf is one partner as the page shows it.
func (s *Service) partnerViewOf(ctx context.Context, userID string, owner *pool, value *member) (partnerView, error) {
	view := partnerView{MemberID: value.MemberID, Name: value.Name, IsHost: value.MemberID == owner.HostMemberID}
	link, err := s.linkToMember(ctx, userID, owner.PoolID, value.MemberID)
	if err != nil {
		return view, err
	}
	var (
		status, statusMessage *string
		accountEnabled        *int
		models                *int64
	)
	if link != nil && link.AccountID != nil {
		var (
			state   string
			message *string
			enabled int
			count   int64
		)
		err := s.DB.R.QueryRowContext(ctx, `SELECT a.status, a.status_message, a.enabled,
			(SELECT COUNT(*) FROM models m WHERE m.account_id = a.id AND m.available = 1 AND m.capability = 'chat')
			FROM provider_accounts a WHERE a.id = ?`, *link.AccountID).Scan(&state, &message, &enabled, &count)
		if err != nil && !isNoRows(err) {
			return view, fmt.Errorf("pools: read a partner's connection: %w", err)
		}
		if err == nil {
			status, statusMessage, accountEnabled, models = &state, message, &enabled, &count
		}
	}
	now := s.nowMS()
	if link != nil {
		balance := s.balanceWith(ctx, link.ID, now)
		view.Balance = &partnerBalance{State: balance.State, Factor: balance.Factor,
			Week: balanceWeek{Given: balance.Given, Taken: balance.Taken}}
		given, err := s.trafficToday(ctx, link.ID, "given", now)
		if err != nil {
			return view, err
		}
		taken, err := s.trafficToday(ctx, link.ID, "taken", now)
		if err != nil {
			return view, err
		}
		view.Today = &partnerToday{Given: given, Taken: taken}
		caps := s.capsFor(ctx, link.ID, shareSettingsOf(owner), now)
		view.Caps = &partnerCaps{RequestsPerDay: caps.Requests, TokensPerDay: caps.Tokens, Concurrent: caps.Concurrent}
	}
	view.ViaHost = isRelayAddress(value.URL)
	if view.ViaHost {
		if online, known := s.relayOnline(ctx, userID, owner.PoolID, value.MemberID); known && !online {
			view.RelayDown = true
		}
	}
	offline := status == nil || *status == "error" || s.PartnerDown(accountOf(link), now)
	view.LinkID = linkIDOf(link)
	problem := ""
	if link != nil && link.LastError != nil {
		problem = *link.LastError
	} else if status != nil && *status == "error" && statusMessage != nil {
		problem = *statusMessage
	}
	view.Problem = nil
	if problem != "" {
		view.Problem = &problem
	}
	if models != nil {
		view.Models = *models
	}
	switch {
	case link == nil:
		view.State = "connecting"
	case link.Status == "paused" || owner.Paused:
		view.State = "paused"
	case offline || view.RelayDown:
		view.State = "offline"
	default:
		view.State = "online"
	}
	_ = accountEnabled
	return view, nil
}

// accountOf is a link's connection id, "" when it has none.
func accountOf(link *peerLink) string {
	if link == nil || link.AccountID == nil {
		return ""
	}
	return *link.AccountID
}

// linkIDOf is a link's id, nil when there is no link.
func linkIDOf(link *peerLink) *string {
	if link == nil {
		return nil
	}
	id := link.ID
	return &id
}

// poolViewOf is one pool as the page shows it.
func (s *Service) poolViewOf(ctx context.Context, userID string, owner *pool, canManage bool) (poolView, error) {
	record, err := s.ensureIdentity(ctx, userID)
	if err != nil {
		return poolView{}, err
	}
	view := poolView{
		ID: owner.PoolID, Name: owner.Name, IsHost: owner.IsHost, Paused: owner.Paused,
		SelfName: owner.SelfName,
		Relay: poolRelayView{
			Offered: relayed(owner),
			ViaHost: isRelayAddress(owner.SelfURL),
		},
		JoinedAt:   owner.JoinedAt,
		LastSyncAt: owner.LastSyncAt,
		Share:      shareSettingsOf(owner),
		Partners:   []partnerView{},
		Invites:    []inviteView{},
	}
	if !owner.IsHost {
		connected := s.connectorConnected(userID, owner.PoolID)
		view.Relay.Connected = &connected
	}
	members, err := s.members(ctx, userID, owner.PoolID)
	if err != nil {
		return poolView{}, err
	}
	for _, value := range members {
		if value.MemberID == record.MemberID {
			continue
		}
		partner, err := s.partnerViewOf(ctx, userID, owner, value)
		if err != nil {
			return poolView{}, err
		}
		view.Partners = append(view.Partners, partner)
	}
	shared, err := s.sharedAccountIDs(ctx, userID, owner.PoolID)
	if err != nil {
		return poolView{}, err
	}
	view.SharedAccountIDs = shared
	kinds, err := s.shareKindsByAccount(ctx, userID, owner.PoolID)
	if err != nil {
		return poolView{}, err
	}
	view.ShareKinds = kinds
	if owner.IsHost && canManage {
		invites, err := s.invites(ctx, userID, owner.PoolID)
		if err != nil {
			return poolView{}, err
		}
		for _, value := range invites {
			view.Invites = append(view.Invites, inviteView{
				ID: value.ID, ExpiresAt: isoMillis(value.ExpiresAt), CreatedAt: value.CreatedAt,
			})
		}
	}
	return view, nil
}

// summary is the whole pool page state for one person.
func (s *Service) summary(ctx context.Context, userID string, canManage bool) (*Summary, error) {
	owned, err := s.pools(ctx, userID)
	if err != nil {
		return nil, err
	}
	result := &Summary{
		Pools:     []poolView{},
		Privacy:   s.PoolPrivacySettings(ctx, userID),
		Shareable: []ShareableAccount{},
		Defaults: ShareDefaults{
			RequestsPerDay: jsNum(AutoShare.RequestsPerDay),
			TokensPerDay:   jsNum(AutoShare.TokensPerDay),
			Concurrent:     jsNum(AutoShare.Concurrent),
			Balance:        ShareBalanceLimits{Floor: Balance.Floor, Ceiling: Balance.Ceiling},
		},
		MaxPools: MaxPools,
	}
	for _, owner := range owned {
		view, err := s.poolViewOf(ctx, userID, owner, canManage)
		if err != nil {
			return nil, err
		}
		result.Pools = append(result.Pools, view)
	}
	if canManage {
		shareable, err := s.shareableAccounts(ctx, userID)
		if err != nil {
			return nil, err
		}
		result.Shareable = shareable
	}
	return result, nil
}

// updateShare changes what a pool shares: the fixed caps and which connections.
func (s *Service) updateShare(ctx context.Context, userID, poolID string, input map[string]any) (*Summary, error) {
	owner, err := s.requirePool(ctx, userID, poolID)
	if err != nil {
		return nil, err
	}
	next := map[string]any{}
	for key, value := range owner.Share {
		next[key] = value
	}
	// A number fixes that cap; null hands it back to the automatic share.
	for _, field := range shareFields {
		value, present := input[field]
		if !present {
			continue
		}
		if value == nil {
			delete(next, field)
			continue
		}
		next[field] = value
	}
	settings := shareSettingsOf(&pool{Share: next})
	filtered := map[string]any{}
	for _, field := range shareFields {
		value, set := settings.field(field)
		if !set {
			continue
		}
		filtered[field] = value
	}
	shared, hasShared := input["sharedAccountIds"].([]any)
	kinds := map[string][]string{}
	if rawKinds, ok := input["shareKinds"].(map[string]any); ok {
		for accountID, raw := range rawKinds {
			list, _ := raw.([]any)
			names := []string{}
			for _, item := range list {
				if text, ok := item.(string); ok {
					names = append(names, text)
				}
			}
			kinds[accountID] = names
		}
	}
	if err := s.updatePool(ctx, userID, poolID, poolUpdate{Share: filtered}); err != nil {
		return nil, err
	}
	if hasShared {
		ids := []string{}
		for _, item := range shared {
			if text, ok := item.(string); ok {
				ids = append(ids, text)
			}
		}
		if err := s.setSharedAccounts(ctx, userID, poolID, ids, kinds); err != nil {
			return nil, err
		}
	}
	if err := s.announceShared(ctx, userID, poolID); err != nil {
		s.log().Warn("Could not tell partners about a share change", "error", err)
	}
	return s.summary(ctx, userID, true)
}

// setAccountShared shares one connection in one pool: shared on or off, and which
// kinds of its models.
func (s *Service) setAccountShared(ctx context.Context, userID, poolID, accountID string, change shareChange) (*Summary, error) {
	if _, err := s.requirePool(ctx, userID, poolID); err != nil {
		return nil, err
	}
	shared, err := s.SetAccountShare(ctx, userID, poolID, accountID, change)
	if err != nil {
		return nil, err
	}
	if !shared {
		return nil, fail(http.StatusNotFound, "ACCOUNT_NOT_FOUND", "This connection does not exist.")
	}
	if err := s.announceShared(ctx, userID, poolID); err != nil {
		s.log().Warn("Could not tell partners about a share change", "error", err)
	}
	return s.summary(ctx, userID, true)
}

// setPoolPaused is the quick switch: off pauses giving and using in this pool at
// once; on restores each partner as it was.
func (s *Service) setPoolPaused(ctx context.Context, userID, poolID string, paused bool) (*Summary, error) {
	if _, err := s.requirePool(ctx, userID, poolID); err != nil {
		return nil, err
	}
	if err := s.updatePool(ctx, userID, poolID, poolUpdate{Paused: &paused}); err != nil {
		return nil, err
	}
	links, err := s.links(ctx, userID, poolID)
	if err != nil {
		return nil, err
	}
	for _, link := range links {
		s.applyEnabled(ctx, userID, link)
		if !paused && link.AccountID != nil && s.MarkFixed != nil {
			s.MarkFixed(*link.AccountID, s.now())
		}
	}
	// Partners refresh what we offer: nothing while paused, everything again after.
	if err := s.announceShared(ctx, userID, poolID); err != nil {
		s.log().Warn("Could not tell partners about the pause", "error", err)
	}
	return s.summary(ctx, userID, true)
}

// setRelayEnabled is the host's switch: pass traffic for members without a public
// address, or not.
func (s *Service) setRelayEnabled(ctx context.Context, userID, poolID string, enabled bool) (*Summary, error) {
	if _, err := s.requireHost(ctx, userID, poolID); err != nil {
		return nil, err
	}
	if err := s.updatePool(ctx, userID, poolID, poolUpdate{RelayEnabled: &enabled}); err != nil {
		return nil, err
	}
	if !enabled {
		s.hub.closePool(poolID)
	}
	return s.summary(ctx, userID, true)
}

// setPartnerPaused pauses giving to and taking from one partner.
func (s *Service) setPartnerPaused(ctx context.Context, userID, linkID string, paused bool) (*Summary, error) {
	link, err := s.link(ctx, userID, linkID)
	if err != nil {
		return nil, err
	}
	if link == nil {
		return nil, fail(http.StatusNotFound, "POOL_PARTNER_NOT_FOUND", "This partner does not exist.")
	}
	if err := s.setPaused(ctx, userID, link, paused); err != nil {
		return nil, err
	}
	if !paused && link.AccountID != nil && s.MarkFixed != nil {
		s.MarkFixed(*link.AccountID, s.now())
	}
	partner, err := s.member(ctx, userID, link.PoolID, link.MemberID)
	if err != nil {
		return nil, err
	}
	if partner != nil {
		if err := s.notify(ctx, userID, link.PoolID, []*member{partner}, "shared_changed", nil); err != nil {
			s.log().Warn("Could not tell a partner about the pause", "error", err)
		}
	}
	return s.summary(ctx, userID, true)
}

// announceShared tells every partner in this pool to refresh our models.
func (s *Service) announceShared(ctx context.Context, userID, poolID string) error {
	others, err := s.othersIn(ctx, userID, poolID)
	if err != nil {
		return err
	}
	return s.notify(ctx, userID, poolID, others, "shared_changed", nil)
}

// othersIn lists the other members of one pool.
func (s *Service) othersIn(ctx context.Context, userID, poolID string) ([]*member, error) {
	record, err := s.ensureIdentity(ctx, userID)
	if err != nil {
		return nil, err
	}
	members, err := s.members(ctx, userID, poolID)
	if err != nil {
		return nil, err
	}
	others := []*member{}
	for _, value := range members {
		if value.MemberID != record.MemberID {
			others = append(others, value)
		}
	}
	return others, nil
}

// notify sends one message to every member it names, ignoring the ones that cannot
// be reached.
func (s *Service) notify(ctx context.Context, userID, poolID string, members []*member, messageType string, data map[string]any) error {
	payload := map[string]any{"pool": poolID}
	for key, value := range data {
		payload[key] = value
	}
	for _, target := range members {
		targetID := target.MemberID
		message, err := s.envelope(ctx, userID, messageType, payload, &targetID)
		if err != nil {
			s.log().Warn("Could not sign a pool message", "error", err)
			continue
		}
		if _, err := s.post(ctx, userID, target.URL, messageType, message); err != nil {
			s.log().Debug("A pool partner did not answer", "memberId", target.MemberID, "error", err)
		}
	}
	return nil
}

// Leave takes this person out of a pool and tells the other members; a host
// leaving closes the pool for everyone. A partner that cannot be reached does
// not keep them in it.
func (s *Service) Leave(ctx context.Context, userID, poolID string) error {
	owner, err := s.requirePool(ctx, userID, poolID)
	if err != nil {
		return err
	}
	others, err := s.othersIn(ctx, userID, poolID)
	if err != nil {
		return err
	}
	messageType := "leave"
	if owner.IsHost {
		messageType = "dissolve"
	}
	if err := s.notify(ctx, userID, poolID, others, messageType, nil); err != nil {
		return err
	}
	return s.leaveLocally(ctx, userID, poolID)
}

// removeMember removes one member from a pool this person hosts.
func (s *Service) removeMember(ctx context.Context, userID, poolID, memberID string) error {
	if _, err := s.requireHost(ctx, userID, poolID); err != nil {
		return err
	}
	record, err := s.ensureIdentity(ctx, userID)
	if err != nil {
		return err
	}
	target, err := s.member(ctx, userID, poolID, memberID)
	if err != nil {
		return err
	}
	if target == nil || memberID == record.MemberID {
		return fail(http.StatusNotFound, "POOL_MEMBER_UNKNOWN", "This pool member does not exist.")
	}
	// Their access ends at once; they are told while they can still be reached
	// (through us, if they have no public address), then they are gone.
	link, err := s.linkToMember(ctx, userID, poolID, memberID)
	if err != nil {
		return err
	}
	if link != nil {
		s.teardown(ctx, userID, link)
	}
	if err := s.notify(ctx, userID, poolID, []*member{target}, "remove", map[string]any{"memberId": memberID}); err != nil {
		return err
	}
	if err := s.dropMember(ctx, userID, poolID, memberID); err != nil {
		return err
	}
	others, err := s.othersIn(ctx, userID, poolID)
	if err != nil {
		return err
	}
	return s.notify(ctx, userID, poolID, others, "remove", map[string]any{"memberId": memberID})
}

// dropMember forgets one member here.
func (s *Service) dropMember(ctx context.Context, userID, poolID, memberID string) error {
	link, err := s.linkToMember(ctx, userID, poolID, memberID)
	if err != nil {
		return err
	}
	if link != nil {
		s.teardown(ctx, userID, link)
	}
	if err := s.removeMemberRow(ctx, userID, poolID, memberID); err != nil {
		return err
	}
	owner, err := s.pool(ctx, userID, poolID)
	if err != nil {
		return err
	}
	if owner != nil && owner.IsHost {
		s.hub.closeMember(poolID, memberID)
	}
	return nil
}

// sync compares with the host's signed member list: links to members who left go,
// members we have no link with yet are introduced.
func (s *Service) sync(ctx context.Context, userID, poolID string) error {
	owner, err := s.pool(ctx, userID, poolID)
	if err != nil {
		return err
	}
	if owner == nil || owner.IsHost {
		return nil
	}
	record, err := s.ensureIdentity(ctx, userID)
	if err != nil {
		return err
	}
	boxKey, boxSig, err := s.boxCard(ctx, userID)
	if err != nil {
		return err
	}
	hostMemberID := owner.HostMemberID
	request, err := s.envelope(ctx, userID, "member_list_request", map[string]any{
		"pool": poolID,
		"self": map[string]any{"boxKey": boxKey, "boxSig": boxSig, "features": poolFeatures},
	}, &hostMemberID)
	if err != nil {
		return err
	}
	answer, err := s.post(ctx, userID, owner.HostURL, "members", request)
	if err != nil {
		return err
	}
	data, err := s.openAnswer(answer, "member_list", owner.HostPublicKey)
	if err != nil {
		return err
	}
	if stringOf(data["pool"]) != poolID {
		return nil
	}
	hostRelay := offersRelay(listField(data, "features"))
	if hostRelay != owner.HostRelay {
		if err := s.updatePool(ctx, userID, poolID, poolUpdate{HostRelay: &hostRelay}); err != nil {
			return err
		}
	}
	s.ensureRelay(ctx, userID, poolID)
	listed := []*member{}
	for index, raw := range listField(data, "members") {
		if index >= MaxMembers {
			break
		}
		entry, _ := raw.(map[string]any)
		if parsed := sanitizeMember(entry, poolID); parsed != nil {
			listed = append(listed, parsed)
		}
	}
	var self *member
	for _, value := range listed {
		if value.MemberID == record.MemberID {
			self = value
		}
	}
	if self == nil {
		return s.leaveLocally(ctx, userID, poolID)
	}
	listedIDs := map[string]bool{}
	for _, value := range listed {
		listedIDs[value.MemberID] = true
	}
	stored, err := s.members(ctx, userID, poolID)
	if err != nil {
		return err
	}
	for _, value := range stored {
		if !listedIDs[value.MemberID] {
			if err := s.dropMember(ctx, userID, poolID, value.MemberID); err != nil {
				return err
			}
		}
	}
	for _, value := range listed {
		if err := s.rememberMember(ctx, userID, poolID, value); err != nil {
			return err
		}
	}
	if name := stringOf(data["name"]); name != "" {
		cleaned := cleanName(name, owner.Name)
		if err := s.updatePool(ctx, userID, poolID, poolUpdate{Name: &cleaned}); err != nil {
			return err
		}
	}
	for _, value := range listed {
		if value.MemberID == record.MemberID {
			continue
		}
		existing, err := s.linkToMember(ctx, userID, poolID, value.MemberID)
		if err != nil {
			return err
		}
		if existing != nil {
			continue
		}
		if value.MemberID != owner.HostMemberID && value.JoinedAt > self.JoinedAt {
			continue
		}
		current, err := s.pool(ctx, userID, poolID)
		if err != nil {
			return err
		}
		if !canIntroduceTo(current, map[string]any{"features": featuresAny(value.Features)}) {
			continue
		}
		targetID := value.MemberID
		ticketRequest, err := s.envelope(ctx, userID, "ticket_request", map[string]any{
			"pool": poolID, "target": targetID,
		}, &hostMemberID)
		if err != nil {
			return err
		}
		ticketAnswer, err := s.post(ctx, userID, owner.HostURL, "ticket", ticketRequest)
		if err != nil {
			s.log().Warn("Pool introduction during sync failed", "error", err)
			continue
		}
		ticket, err := s.openAnswer(ticketAnswer, "ticket", owner.HostPublicKey)
		if err != nil {
			s.log().Warn("Pool introduction during sync failed", "error", err)
			continue
		}
		if err := s.introduce(ctx, userID, poolID, ticket); err != nil {
			s.log().Warn("Pool introduction during sync failed", "error", err)
		}
	}
	synced := isoMillis(s.nowMS())
	return s.updatePool(ctx, userID, poolID, poolUpdate{LastSyncAt: &synced})
}

// featuresAny turns a feature list into the decoded form canIntroduceTo reads.
func featuresAny(features []string) []any {
	list := make([]any, 0, len(features))
	for _, feature := range features {
		list = append(list, feature)
	}
	return list
}

// fromMember is the recipient, the pool the message names and the sending member,
// after checking the signature.
func (s *Service) fromMember(ctx context.Context, message *peerMessage, messageType string) (string, *pool, *member, map[string]any, error) {
	body, err := message.unverifiedBody()
	if err != nil {
		return "", nil, nil, nil, err
	}
	userID, err := s.userForMember(ctx, stringOf(body["to"]))
	if err != nil {
		return "", nil, nil, nil, err
	}
	var owner *pool
	if userID != "" {
		data, _ := body["data"].(map[string]any)
		owner, err = s.pool(ctx, userID, stringOf(data["pool"]))
		if err != nil {
			return "", nil, nil, nil, err
		}
	}
	var sender *member
	if owner != nil {
		sender, err = s.member(ctx, userID, owner.PoolID, stringOf(body["from"]))
		if err != nil {
			return "", nil, nil, nil, err
		}
	}
	if sender == nil {
		return "", nil, nil, nil, fail(http.StatusNotFound, "POOL_MEMBER_UNKNOWN", "This pool member is not known here.")
	}
	opened, err := s.openEnvelope(message.envelope, messageType, sender.PublicKey, s.nowMS())
	if err != nil {
		return "", nil, nil, nil, err
	}
	return userID, owner, sender, opened.data(), nil
}

// handleMemberList answers a member asking for the pool's member list.
func (s *Service) handleMemberList(ctx context.Context, message *peerMessage) (any, error) {
	userID, owner, sender, data, err := s.fromMember(ctx, message, "member_list_request")
	if err != nil {
		return nil, err
	}
	if !owner.IsHost {
		return nil, fail(http.StatusForbidden, "POOL_HOST_ONLY", "Only the host keeps the member list.")
	}
	// Members that joined before relaying existed hand in their sealing key here.
	rawCard := map[string]any{}
	if self, ok := data["self"].(map[string]any); ok {
		for key, value := range self {
			rawCard[key] = value
		}
	}
	rawCard["memberId"] = sender.MemberID
	rawCard["publicKey"] = sender.PublicKey
	rawCard["url"] = sender.URL
	if card := sanitizeMember(rawCard, owner.PoolID); card != nil && card.BoxKey != "" {
		card.Name = sender.Name
		card.URL = sender.URL
		if err := s.rememberMember(ctx, userID, owner.PoolID, card); err != nil {
			return nil, err
		}
	}
	self, err := s.selfMember(ctx, userID, owner)
	if err != nil {
		return nil, err
	}
	stored := *self
	stored.MemberID = owner.HostMemberID
	if err := s.upsertMember(ctx, userID, owner.PoolID, stored); err != nil {
		return nil, err
	}
	members, err := s.members(ctx, userID, owner.PoolID)
	if err != nil {
		return nil, err
	}
	public := []publicMember{}
	for _, value := range members {
		public = append(public, publicMemberOf(value))
	}
	return s.envelope(ctx, userID, "member_list", map[string]any{
		"pool": owner.PoolID, "name": owner.Name, "members": public, "features": hostFeatures(owner),
	}, &sender.MemberID)
}

// handleTicket answers a member asking to be introduced to another one.
func (s *Service) handleTicket(ctx context.Context, message *peerMessage) (any, error) {
	userID, owner, sender, data, err := s.fromMember(ctx, message, "ticket_request")
	if err != nil {
		return nil, err
	}
	if !owner.IsHost {
		return nil, fail(http.StatusForbidden, "POOL_HOST_ONLY", "Only the host introduces members.")
	}
	target, err := s.member(ctx, userID, owner.PoolID, stringOf(data["target"]))
	if err != nil {
		return nil, err
	}
	if target == nil || target.MemberID == sender.MemberID {
		return nil, fail(http.StatusNotFound, "POOL_MEMBER_UNKNOWN", "This pool member is not known here.")
	}
	ticket, err := s.ticketFor(ctx, userID, owner, sender, target)
	if err != nil {
		return nil, err
	}
	return s.envelope(ctx, userID, "ticket", ticket, &sender.MemberID)
}

// handleLeave handles a member leaving the pool.
func (s *Service) handleLeave(ctx context.Context, message *peerMessage) (any, error) {
	userID, owner, sender, _, err := s.fromMember(ctx, message, "leave")
	if err != nil {
		return nil, err
	}
	if err := s.dropMember(ctx, userID, owner.PoolID, sender.MemberID); err != nil {
		return nil, err
	}
	return map[string]any{"ok": true}, nil
}

// handleRemove handles the host telling this instance a member was removed.
func (s *Service) handleRemove(ctx context.Context, message *peerMessage) (any, error) {
	userID, owner, sender, data, err := s.fromMember(ctx, message, "remove")
	if err != nil {
		return nil, err
	}
	if sender.MemberID != owner.HostMemberID {
		return nil, fail(http.StatusForbidden, "POOL_HOST_ONLY", "Only the host can remove members.")
	}
	record, err := s.ensureIdentity(ctx, userID)
	if err != nil {
		return nil, err
	}
	if stringOf(data["memberId"]) == record.MemberID {
		if err := s.leaveLocally(ctx, userID, owner.PoolID); err != nil {
			return nil, err
		}
	} else if err := s.dropMember(ctx, userID, owner.PoolID, stringOf(data["memberId"])); err != nil {
		return nil, err
	}
	return map[string]any{"ok": true}, nil
}

// handleDissolve handles the host closing the pool.
func (s *Service) handleDissolve(ctx context.Context, message *peerMessage) (any, error) {
	userID, owner, sender, _, err := s.fromMember(ctx, message, "dissolve")
	if err != nil {
		return nil, err
	}
	if sender.MemberID != owner.HostMemberID {
		return nil, fail(http.StatusForbidden, "POOL_HOST_ONLY", "Only the host can close the pool.")
	}
	if err := s.leaveLocally(ctx, userID, owner.PoolID); err != nil {
		return nil, err
	}
	return map[string]any{"ok": true}, nil
}

// handleSharedChanged refreshes a partner's models now, instead of at the next
// regular model sync.
func (s *Service) handleSharedChanged(ctx context.Context, message *peerMessage) (any, error) {
	userID, owner, sender, _, err := s.fromMember(ctx, message, "shared_changed")
	if err != nil {
		return nil, err
	}
	link, err := s.linkToMember(ctx, userID, owner.PoolID, sender.MemberID)
	if err != nil {
		return nil, err
	}
	s.refreshPartner(ctx, link, false)
	return map[string]any{"ok": true}, nil
}
