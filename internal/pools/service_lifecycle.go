package pools

// Starting a pool, inviting, joining and introducing members
// (pool_service.js: creating, joining, introductions).

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/google/uuid"
)

// requirePool is one person's pool, or the error the API answers with.
func (s *Service) requirePool(ctx context.Context, userID, poolID string) (*pool, error) {
	found, err := s.pool(ctx, userID, poolID)
	if err != nil {
		return nil, err
	}
	if found == nil {
		return nil, fail(http.StatusNotFound, "POOL_NOT_FOUND", "This pool does not exist, or you are not in it.")
	}
	return found, nil
}

// requireHost is one pool this person hosts.
func (s *Service) requireHost(ctx context.Context, userID, poolID string) (*pool, error) {
	found, err := s.requirePool(ctx, userID, poolID)
	if err != nil {
		return nil, err
	}
	if !found.IsHost {
		return nil, fail(http.StatusForbidden, "POOL_HOST_ONLY", "Only the pool’s host can do this.")
	}
	return found, nil
}

// assertRoomForPool refuses a new pool once the person has MaxPools.
func (s *Service) assertRoomForPool(ctx context.Context, userID string) error {
	owned, err := s.pools(ctx, userID)
	if err != nil {
		return err
	}
	if len(owned) >= MaxPools {
		return fail(http.StatusConflict, "POOL_LIMIT", fmt.Sprintf("You can be in at most %d pools.", MaxPools))
	}
	return nil
}

// ownOrigin is this NeoRouter's public address, or the error that says it is
// unknown.
func ownOrigin(selfURL string) (string, error) {
	origin := originOf(selfURL)
	if origin == "" {
		return "", fail(http.StatusBadRequest, "POOL_URL_UNKNOWN",
			"This NeoRouter’s public address is unknown. Set NEOROUTER_PUBLIC_URL so partners can reach it.")
	}
	return origin, nil
}

// poolCreate is a request to start a pool.
type poolCreate struct {
	Name             string
	SelfName         string
	SelfURL          string
	SharedAccountIDs []string
}

// create starts a pool this person hosts.
func (s *Service) create(ctx context.Context, userID string, input poolCreate) (*createResult, error) {
	if err := s.assertRoomForPool(ctx, userID); err != nil {
		return nil, err
	}
	origin, err := ownOrigin(input.SelfURL)
	if err != nil {
		return nil, err
	}
	record, err := s.ensureIdentity(ctx, userID)
	if err != nil {
		return nil, err
	}
	ownName := cleanName(input.SelfName, "Me")
	poolID := uuid.NewString()
	if err := s.insertPool(ctx, poolInput{
		UserID: userID, PoolID: poolID, Name: cleanName(input.Name, "My pool"), IsHost: true,
		HostMemberID: record.MemberID, HostPublicKey: record.PublicKey, HostURL: origin,
		SelfURL: origin, SelfName: ownName, Share: map[string]any{},
	}); err != nil {
		return nil, err
	}
	boxKey, boxSig, err := s.boxCard(ctx, userID)
	if err != nil {
		return nil, err
	}
	if err := s.upsertMember(ctx, userID, poolID, member{
		MemberID: record.MemberID, Name: ownName, URL: origin, PublicKey: record.PublicKey,
		BoxKey: boxKey, BoxSig: boxSig, Features: append([]string(nil), poolFeatures...),
	}); err != nil {
		return nil, err
	}
	if err := s.setSharedAccounts(ctx, userID, poolID, input.SharedAccountIDs, nil); err != nil {
		return nil, err
	}
	summary, err := s.summary(ctx, userID, true)
	if err != nil {
		return nil, err
	}
	return &createResult{PoolID: poolID, Summary: *summary}, nil
}

// inviteResult is a new invite link.
type inviteResult struct {
	ID        string `json:"id"`
	Link      string `json:"link"`
	ExpiresAt string `json:"expiresAt"`
}

// createInvite makes a single-use invite link for a pool this person hosts.
func (s *Service) createInvite(ctx context.Context, userID, poolID string) (*inviteResult, error) {
	owner, err := s.requireHost(ctx, userID, poolID)
	if err != nil {
		return nil, err
	}
	members, err := s.members(ctx, userID, poolID)
	if err != nil {
		return nil, err
	}
	if len(members) >= MaxMembers {
		return nil, fail(http.StatusConflict, "POOL_FULL", fmt.Sprintf("A pool has at most %d members.", MaxMembers))
	}
	open, err := s.activeInviteCount(ctx, userID, poolID)
	if err != nil {
		return nil, err
	}
	if open >= MaxActiveInvites {
		return nil, fail(http.StatusTooManyRequests, "POOL_TOO_MANY_INVITES", "Too many open invites. Revoke some first.")
	}
	token, err := randomToken(24)
	if err != nil {
		return nil, err
	}
	expiresAt := s.nowMS() + InviteTTL.Milliseconds()
	id, err := s.insertInvite(ctx, userID, poolID, hashToken(token), expiresAt)
	if err != nil {
		return nil, err
	}
	// Opens a page on this server that sends the invited person to their own
	// NeoRouter.
	return &inviteResult{ID: id, Link: owner.SelfURL + "/pool/join?pool=" + token, ExpiresAt: isoMillis(expiresAt)}, nil
}

// revokeInvite takes back one open invite link.
func (s *Service) revokeInvite(ctx context.Context, userID, poolID, inviteID string) error {
	if _, err := s.requireHost(ctx, userID, poolID); err != nil {
		return err
	}
	revoked, err := s.revokeInviteRow(ctx, userID, poolID, inviteID)
	if err != nil {
		return err
	}
	if !revoked {
		return fail(http.StatusNotFound, "POOL_INVITE_NOT_FOUND", "This invite does not exist.")
	}
	return nil
}

// inviteLink is a parsed invite link: which server, which token.
type inviteLink struct {
	Origin string
	Token  string
}

// parseInvite reads an invite link.
func parseInvite(link string) (inviteLink, error) {
	parsed, err := url.Parse(strings.TrimSpace(link))
	if err != nil {
		return inviteLink{}, fail(http.StatusBadRequest, "POOL_INVITE_INVALID", "This is not a pool invite link.")
	}
	token := parsed.Query().Get("pool")
	origin := ""
	if parsed.Scheme != "" && parsed.Host != "" {
		origin = originOf(parsed.Scheme + "://" + parsed.Host)
	}
	if origin == "" || token == "" || !tokenText.MatchString(token) {
		return inviteLink{}, fail(http.StatusBadRequest, "POOL_INVITE_INVALID", "This is not a pool invite link.")
	}
	return inviteLink{Origin: origin, Token: token}, nil
}

// validInvite is the open invite a token names and the pool it belongs to.
func (s *Service) validInvite(ctx context.Context, token string) (*invite, *pool, error) {
	var found *invite
	if tokenText.MatchString(token) {
		loaded, err := s.inviteByHash(ctx, hashToken(token))
		if err != nil {
			return nil, nil, err
		}
		found = loaded
	}
	var owner *pool
	if found != nil {
		loaded, err := s.pool(ctx, found.UserID, found.PoolID)
		if err != nil {
			return nil, nil, err
		}
		owner = loaded
	}
	if found == nil || found.UsedAt != nil || found.RevokedAt != nil || found.ExpiresAt <= s.nowMS() ||
		owner == nil || !owner.IsHost {
		return nil, nil, fail(http.StatusNotFound, "POOL_INVITE_INVALID",
			"This invite link is no longer valid. Ask for a new one.")
	}
	return found, owner, nil
}

// previewResult is what the join dialog shows about an invite.
type previewResult struct {
	URL           string        `json:"url"`
	Pool          previewName   `json:"pool"`
	Host          previewName   `json:"host"`
	Members       []previewName `json:"members"`
	AlreadyMember bool          `json:"alreadyMember"`
	Relay         bool          `json:"relay"`
}

// previewName is one name the join dialog shows.
type previewName struct {
	Name string `json:"name"`
}

// preview asks the host what an invite link leads to.
func (s *Service) preview(ctx context.Context, userID, link string) (*previewResult, error) {
	invite, err := parseInvite(link)
	if err != nil {
		return nil, err
	}
	answer, err := s.post(ctx, userID, invite.Origin, "preview", map[string]any{"token": invite.Token})
	if err != nil {
		return nil, err
	}
	result := &previewResult{
		URL:     invite.Origin,
		Pool:    previewName{Name: cleanName(stringFieldOf(objectField(answer, "pool"), "name"), "Pool")},
		Host:    previewName{Name: cleanName(stringFieldOf(objectField(answer, "host"), "name"), "Host")},
		Members: []previewName{},
		Relay:   offersRelay(listField(answer, "features")),
	}
	for _, raw := range listField(answer, "members") {
		if len(result.Members) >= MaxMembers {
			break
		}
		result.Members = append(result.Members, previewName{Name: cleanName(stringFieldOf(raw, "name"), "Partner")})
	}
	if id := stringFieldOf(objectField(answer, "pool"), "id"); id != "" {
		found, err := s.pool(ctx, userID, id)
		if err != nil {
			return nil, err
		}
		result.AlreadyMember = found != nil
	}
	return result, nil
}

// stringFieldOf is one string field of a decoded JSON value.
func stringFieldOf(value any, key string) string {
	text, _ := stringField(value, key)
	return text
}

// previewAnswer is what the host tells a joiner about an invite.
type previewAnswer struct {
	V        int            `json:"v"`
	Pool     previewPool    `json:"pool"`
	Host     previewHost    `json:"host"`
	Members  []previewName2 `json:"members"`
	Features []string       `json:"features"`
}

type previewPool struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type previewHost struct {
	MemberID  string `json:"memberId"`
	Name      string `json:"name"`
	PublicKey string `json:"publicKey"`
}

type previewName2 struct {
	Name string `json:"name"`
}

// handlePreview answers a joiner's question about an invite token.
func (s *Service) handlePreview(ctx context.Context, token string) (*previewAnswer, error) {
	_, owner, err := s.validInvite(ctx, token)
	if err != nil {
		return nil, err
	}
	members, err := s.members(ctx, owner.UserID, owner.PoolID)
	if err != nil {
		return nil, err
	}
	answer := &previewAnswer{
		V:        ProtocolVersion,
		Pool:     previewPool{ID: owner.PoolID, Name: owner.Name},
		Host:     previewHost{MemberID: owner.HostMemberID, Name: owner.SelfName, PublicKey: owner.HostPublicKey},
		Members:  []previewName2{},
		Features: hostFeatures(owner),
	}
	for _, member := range members {
		answer.Members = append(answer.Members, previewName2{Name: member.Name})
	}
	return answer, nil
}

// accept links us with a partner whose key we were given: a new link, a key for
// them, and the partner as a connection. It replaces an older link to them.
func (s *Service) accept(ctx context.Context, userID, poolID string, value *member, peerKey string) (string, error) {
	if err := assertPeerKey(peerKey); err != nil {
		return "", err
	}
	existing, err := s.linkToMember(ctx, userID, poolID, value.MemberID)
	if err != nil {
		return "", err
	}
	if existing != nil {
		s.teardown(ctx, userID, existing)
	}
	link, err := s.insertLink(ctx, userID, poolID, *value)
	if err != nil {
		return "", err
	}
	ourKey, err := s.issueInboundKey(ctx, userID, link)
	if err != nil {
		return "", err
	}
	loaded, err := s.link(ctx, userID, link.ID)
	if err != nil {
		return "", err
	}
	s.connectOutbound(ctx, userID, loaded, peerKey)
	return ourKey, nil
}

// introTicket is one signed introduction ticket.
type introTicket struct {
	Pool    string `json:"pool"`
	From    string `json:"from"`
	FromKey string `json:"fromKey"`
	To      string `json:"to"`
	Exp     int64  `json:"exp"`
}

// ticketAnswer is the ticket and the introduction it authorises.
type ticketAnswer struct {
	Ticket    introTicket  `json:"ticket"`
	Signature string       `json:"signature"`
	Member    publicMember `json:"member"`
}

// ticketFor signs one introduction of a new member to an existing one.
func (s *Service) ticketFor(ctx context.Context, hostUserID string, owner *pool, newMember, target *member) (*ticketAnswer, error) {
	ticket := introTicket{
		Pool: owner.PoolID, From: newMember.MemberID, FromKey: newMember.PublicKey,
		To: target.MemberID, Exp: s.nowMS() + TicketTTL.Milliseconds(),
	}
	signature, err := s.signObject(ctx, hostUserID, ticket)
	if err != nil {
		return nil, err
	}
	return &ticketAnswer{Ticket: ticket, Signature: signature, Member: publicMemberOf(target)}, nil
}

// poolJoin is a request to join a pool with an invite link.
type poolJoin struct {
	Link             string
	SelfName         string
	SelfURL          string
	SharedAccountIDs []string
}

// join joins a pool through an invite link.
func (s *Service) join(ctx context.Context, userID string, input poolJoin) (*createResult, error) {
	if err := s.assertRoomForPool(ctx, userID); err != nil {
		return nil, err
	}
	invite, err := parseInvite(input.Link)
	if err != nil {
		return nil, err
	}
	info, err := s.post(ctx, userID, invite.Origin, "preview", map[string]any{"token": invite.Token})
	if err != nil {
		return nil, err
	}
	if version := numberField(objectOf(info), "v"); version != ProtocolVersion {
		return nil, fail(http.StatusBadRequest, "POOL_VERSION_MISMATCH",
			"The host speaks another pool protocol version. Update both NeoRouters to the latest release.")
	}
	poolID := strings.ToLower(stringFieldOf(objectField(info, "pool"), "id"))
	hostRelay := offersRelay(listField(info, "features"))
	// Without a public address we can still join a host that relays: we offer what
	// we have and the host decides (welcome.you).
	ownURL := originOf(input.SelfURL)
	if ownURL == "" && !hostRelay {
		if _, err := ownOrigin(input.SelfURL); err != nil {
			return nil, err
		}
	}
	hostRaw := map[string]any{}
	for key, value := range objectOf(objectField(info, "host")) {
		hostRaw[key] = value
	}
	hostRaw["url"] = invite.Origin
	host := sanitizeMember(hostRaw, poolID)
	if host == nil || !memberIDText.MatchString(poolID) {
		return nil, fail(http.StatusBadGateway, "POOL_PARTNER_REFUSED", "The host sent an invalid answer.")
	}
	record, err := s.ensureIdentity(ctx, userID)
	if err != nil {
		return nil, err
	}
	if host.MemberID == record.MemberID {
		return nil, fail(http.StatusBadRequest, "POOL_OWN_INVITE", "This is an invite to your own pool.")
	}
	if existing, err := s.pool(ctx, userID, poolID); err != nil {
		return nil, err
	} else if existing != nil {
		return nil, fail(http.StatusConflict, "POOL_ALREADY_JOINED", "You are already in this pool.")
	}
	ownName := cleanName(input.SelfName, "Partner")
	// Saved before the handshake: the host lists our shared models while it
	// answers, and our side must already know the pool for that.
	selfURL := ownURL
	if selfURL == "" {
		selfURL = formatRelayAddress(poolID, record.MemberID)
	}
	if err := s.insertPool(ctx, poolInput{
		UserID: userID, PoolID: poolID, Name: cleanName(stringFieldOf(objectField(info, "pool"), "name"), "Pool"),
		IsHost: false, HostMemberID: host.MemberID, HostPublicKey: host.PublicKey, HostURL: invite.Origin,
		SelfURL: selfURL, SelfName: ownName, Share: map[string]any{}, HostRelay: hostRelay,
	}); err != nil {
		return nil, err
	}
	if err := s.setSharedAccounts(ctx, userID, poolID, input.SharedAccountIDs, nil); err != nil {
		return nil, err
	}
	hostLink, err := s.insertLink(ctx, userID, poolID, *host)
	if err != nil {
		return nil, err
	}
	var welcome map[string]any
	peerKey := ""
	if err := func() error {
		created, err := s.issueInboundKey(ctx, userID, hostLink)
		if err != nil {
			return err
		}
		peerKey = created
		self, err := s.selfMember(ctx, userID, &pool{PoolID: poolID, SelfName: ownName, SelfURL: selfURL})
		if err != nil {
			return err
		}
		message, err := s.envelope(ctx, userID, "join", map[string]any{
			"pool": poolID, "token": invite.Token, "member": selfData(self), "peerKey": peerKey,
		}, &host.MemberID)
		if err != nil {
			return err
		}
		answer, err := s.post(ctx, userID, invite.Origin, "join", message)
		if err != nil {
			return err
		}
		opened, err := s.openAnswer(answer, "welcome", host.PublicKey)
		if err != nil {
			return err
		}
		welcome = opened
		key := stringOf(welcome["peerKey"])
		if err := assertPeerKey(key); err != nil {
			return err
		}
		peerKey = key
		if strings.ToLower(stringFieldOf(objectField(welcome, "pool"), "id")) != poolID {
			return fail(http.StatusBadGateway, "POOL_PARTNER_REFUSED", "The host answered for another pool.")
		}
		return nil
	}(); err != nil {
		if leaveErr := s.leaveLocally(ctx, userID, poolID); leaveErr != nil {
			s.log().Warn("Could not roll back a failed join", "error", leaveErr)
		}
		return nil, err
	}
	// The address the host settled on: ours, or our relay address.
	settled := originOf(stringFieldOf(objectField(welcome, "you"), "url"))
	if settled != "" && (settled == ownURL || settled == formatRelayAddress(poolID, record.MemberID)) {
		if err := s.updatePool(ctx, userID, poolID, poolUpdate{SelfURL: &settled}); err != nil {
			return nil, err
		}
	}
	hostRelay = offersRelay(listField(welcome, "features"))
	if err := s.updatePool(ctx, userID, poolID, poolUpdate{HostRelay: &hostRelay}); err != nil {
		return nil, err
	}
	for _, raw := range listField(welcome, "members") {
		entry, _ := raw.(map[string]any)
		if parsed := sanitizeMember(entry, poolID); parsed != nil {
			if err := s.rememberMember(ctx, userID, poolID, parsed); err != nil {
				return nil, err
			}
		}
	}
	// Partners reach a relayed member (and it reaches relayed partners) only once
	// it is connected to the host.
	if s.ensureRelay(ctx, userID, poolID) != nil {
		s.waitRelayReady(userID, poolID, RelayReadyTimeout)
	}
	loaded, err := s.link(ctx, userID, hostLink.ID)
	if err != nil {
		return nil, err
	}
	s.connectOutbound(ctx, userID, loaded, peerKey)
	tickets := listField(welcome, "tickets")
	for index, raw := range tickets {
		if index >= MaxMembers {
			break
		}
		ticket, _ := raw.(map[string]any)
		current, err := s.pool(ctx, userID, poolID)
		if err != nil {
			return nil, err
		}
		if !canIntroduceTo(current, ticket["member"]) {
			continue
		}
		if err := s.introduce(ctx, userID, poolID, ticket); err != nil {
			s.log().Warn("Introduction to a pool member failed; sync will try again", "error", err)
		}
	}
	summary, err := s.summary(ctx, userID, true)
	if err != nil {
		return nil, err
	}
	return &createResult{PoolID: poolID, Summary: *summary}, nil
}

// openAnswer verifies one answer and returns its data.
func (s *Service) openAnswer(answer any, messageType, publicKey string) (map[string]any, error) {
	envelope, err := envelopeOf(answer)
	if err != nil {
		return nil, err
	}
	body, err := s.openEnvelope(envelope, messageType, publicKey, s.nowMS())
	if err != nil {
		return nil, err
	}
	return body.data(), nil
}

// envelopeOf reads a decoded JSON value as a signed envelope.
func envelopeOf(value any) (*peerEnvelope, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, fail(http.StatusBadGateway, "POOL_PARTNER_REFUSED", "The host sent an invalid answer.")
	}
	envelope := &peerEnvelope{}
	if err := json.Unmarshal(encoded, envelope); err != nil {
		return nil, fail(http.StatusBadGateway, "POOL_PARTNER_REFUSED", "The host sent an invalid answer.")
	}
	return envelope, nil
}

// selfData is one member as the pool messages carry it.
func selfData(value *member) map[string]any {
	data := map[string]any{
		"memberId": value.MemberID, "name": value.Name, "url": value.URL,
		"publicKey": value.PublicKey, "features": value.Features,
	}
	if value.BoxKey != "" {
		data["boxKey"] = value.BoxKey
		data["boxSig"] = value.BoxSig
	}
	if value.JoinedAt != "" {
		data["joinedAt"] = value.JoinedAt
	}
	return data
}

// handleJoin is the host's side of a join: it checks the invite, adds the member
// and answers with keys, the member list and one ticket per other member.
func (s *Service) handleJoin(ctx context.Context, message *peerMessage) (any, error) {
	data, _ := message.unverifiedData()
	invite, owner, err := s.validInvite(ctx, stringOf(data["token"]))
	if err != nil {
		return nil, err
	}
	hostUserID := owner.UserID
	rawMember, _ := data["member"].(map[string]any)
	joiner := sanitizeMember(rawMember, owner.PoolID)
	if joiner == nil {
		return nil, fail(http.StatusBadRequest, "POOL_MESSAGE_INVALID", "The member details are malformed.")
	}
	body, err := s.openEnvelope(message.envelope, "join", joiner.PublicKey, s.nowMS())
	if err != nil {
		return nil, err
	}
	opened := body.data()
	if body.From != joiner.MemberID || body.To == nil || *body.To != owner.HostMemberID ||
		stringOf(opened["pool"]) != owner.PoolID {
		return nil, fail(http.StatusBadRequest, "POOL_MESSAGE_INVALID", "The message is addressed wrongly.")
	}
	existing, err := s.member(ctx, hostUserID, owner.PoolID, joiner.MemberID)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		return nil, fail(http.StatusConflict, "POOL_ALREADY_MEMBER", "You are already in this pool.")
	}
	members, err := s.members(ctx, hostUserID, owner.PoolID)
	if err != nil {
		return nil, err
	}
	if len(members) >= MaxMembers {
		return nil, fail(http.StatusConflict, "POOL_FULL", fmt.Sprintf("A pool has at most %d members.", MaxMembers))
	}
	peerKey := stringOf(opened["peerKey"])
	if err := assertPeerKey(peerKey); err != nil {
		return nil, err
	}
	settled, err := s.joinerAddress(ctx, hostUserID, owner, joiner)
	if err != nil {
		return nil, err
	}
	joiner.URL = settled
	used, err := s.useInvite(ctx, invite.ID, joiner.MemberID)
	if err != nil {
		return nil, err
	}
	if !used {
		return nil, fail(http.StatusNotFound, "POOL_INVITE_INVALID", "This invite link is no longer valid. Ask for a new one.")
	}
	ourKey, err := s.accept(ctx, hostUserID, owner.PoolID, joiner, peerKey)
	if err != nil {
		return nil, err
	}
	if err := s.rememberMember(ctx, hostUserID, owner.PoolID, joiner); err != nil {
		return nil, err
	}
	// The host's own entry carries its sealing key too (pools made before relaying
	// lack it).
	self, err := s.selfMember(ctx, hostUserID, owner)
	if err != nil {
		return nil, err
	}
	joined := *self
	joined.MemberID = owner.HostMemberID
	if err := s.upsertMember(ctx, hostUserID, owner.PoolID, joined); err != nil {
		return nil, err
	}
	stored, err := s.member(ctx, hostUserID, owner.PoolID, joiner.MemberID)
	if err != nil {
		return nil, err
	}
	all, err := s.members(ctx, hostUserID, owner.PoolID)
	if err != nil {
		return nil, err
	}
	tickets := []ticketAnswer{}
	for _, other := range all {
		if other.MemberID == owner.HostMemberID || other.MemberID == joiner.MemberID {
			continue
		}
		ticket, err := s.ticketFor(ctx, hostUserID, owner, stored, other)
		if err != nil {
			return nil, err
		}
		tickets = append(tickets, *ticket)
	}
	public := []publicMember{}
	for _, entry := range all {
		public = append(public, publicMemberOf(entry))
	}
	return s.envelope(ctx, hostUserID, "welcome", map[string]any{
		"pool":     map[string]any{"id": owner.PoolID, "name": owner.Name},
		"peerKey":  ourKey,
		"members":  public,
		"tickets":  tickets,
		"features": hostFeatures(owner),
		"you":      map[string]any{"url": joiner.URL},
	}, &joiner.MemberID)
}

// joinerAddress is the address the host keeps for a joiner: its own where that
// works, the relay where it does not and both sides support it, else the joiner's
// own address under the usual rules.
func (s *Service) joinerAddress(ctx context.Context, hostUserID string, owner *pool, joiner *member) (string, error) {
	canRelay := owner.RelayEnabled && containsString(joiner.Features, "relay")
	relayAddr := formatRelayAddress(owner.PoolID, joiner.MemberID)
	if isRelayAddress(joiner.URL) {
		if !canRelay {
			return "", fail(http.StatusBadRequest, "POOL_RELAY_UNAVAILABLE",
				"This pool’s host does not pass traffic for NeoRouters without a public address.")
		}
		return relayAddr, nil
	}
	if !canRelay {
		if err := s.assertReachable(ctx, hostUserID, joiner.URL); err != nil {
			return "", err
		}
		return joiner.URL, nil
	}
	if s.answersDirectly(ctx, hostUserID, joiner.URL) {
		return joiner.URL, nil
	}
	return relayAddr, nil
}

// canIntroduceTo reports whether one member can be introduced to this joiner: a
// member reached only through the host can link with partners that know the relay;
// older NeoRouters would refuse its relay address, so they are skipped.
func canIntroduceTo(owner *pool, raw any) bool {
	if owner == nil || !isRelayAddress(owner.SelfURL) {
		return true
	}
	member, _ := raw.(map[string]any)
	features, _ := member["features"].([]any)
	return offersRelay(features)
}

// introduce introduces this member to one partner with a ticket.
func (s *Service) introduce(ctx context.Context, userID, poolID string, raw map[string]any) error {
	owner, err := s.requirePool(ctx, userID, poolID)
	if err != nil {
		return err
	}
	ticket, _ := raw["ticket"].(map[string]any)
	signature, _ := raw["signature"].(string)
	targetRaw, _ := raw["member"].(map[string]any)
	target := sanitizeMember(targetRaw, poolID)
	if target == nil || target.MemberID != strings.ToLower(stringOf(ticket["to"])) || stringOf(ticket["pool"]) != poolID {
		return fail(http.StatusBadRequest, "POOL_MESSAGE_INVALID", "The introduction is malformed.")
	}
	if err := s.rememberMember(ctx, userID, poolID, target); err != nil {
		return err
	}
	existing, err := s.linkToMember(ctx, userID, poolID, target.MemberID)
	if err != nil {
		return err
	}
	if existing != nil {
		s.teardown(ctx, userID, existing)
	}
	link, err := s.insertLink(ctx, userID, poolID, *target)
	if err != nil {
		return err
	}
	self, err := s.selfMember(ctx, userID, owner)
	if err != nil {
		s.teardown(ctx, userID, link)
		return err
	}
	peerKey, err := s.issueInboundKey(ctx, userID, link)
	if err != nil {
		s.teardown(ctx, userID, link)
		return err
	}
	message, err := s.envelope(ctx, userID, "introduce", map[string]any{
		"pool": poolID, "ticket": ticket, "signature": signature,
		"member": selfData(self), "peerKey": peerKey,
	}, &target.MemberID)
	if err != nil {
		s.teardown(ctx, userID, link)
		return err
	}
	answer, err := s.post(ctx, userID, target.URL, "introduce", message)
	if err != nil {
		s.teardown(ctx, userID, link)
		return err
	}
	opened, err := s.openAnswer(answer, "introduced", target.PublicKey)
	if err != nil {
		s.teardown(ctx, userID, link)
		return err
	}
	replyKey := stringOf(opened["peerKey"])
	if err := assertPeerKey(replyKey); err != nil {
		s.teardown(ctx, userID, link)
		return err
	}
	loaded, err := s.link(ctx, userID, link.ID)
	if err != nil {
		s.teardown(ctx, userID, link)
		return err
	}
	s.connectOutbound(ctx, userID, loaded, replyKey)
	return nil
}

// handleIntroduce answers an introduction with the key to call this member with.
func (s *Service) handleIntroduce(ctx context.Context, message *peerMessage) (any, error) {
	body, err := message.unverifiedBody()
	if err != nil {
		return nil, err
	}
	data, _ := body["data"].(map[string]any)
	ticket, _ := data["ticket"].(map[string]any)
	signature, _ := data["signature"].(string)
	to := stringOf(body["to"])
	userID, err := s.userForMember(ctx, to)
	if err != nil {
		return nil, err
	}
	var owner *pool
	if userID != "" && ticket != nil {
		owner, err = s.pool(ctx, userID, stringOf(ticket["pool"]))
		if err != nil {
			return nil, err
		}
	}
	if owner == nil {
		return nil, fail(http.StatusNotFound, "POOL_MEMBER_UNKNOWN", "This pool member is not on this NeoRouter.")
	}
	exp, _ := ticket["exp"].(json.Number)
	expires, _ := exp.Int64()
	if stringOf(ticket["to"]) != to || stringOf(data["pool"]) != owner.PoolID || expires <= s.nowMS() {
		return nil, fail(http.StatusUnauthorized, "POOL_SIGNATURE_INVALID", "The introduction ticket is not valid.")
	}
	if !verifyObject(owner.HostPublicKey, ticket, signature) {
		return nil, fail(http.StatusUnauthorized, "POOL_SIGNATURE_INVALID",
			"The introduction was not signed by the pool’s host.")
	}
	memberRaw, _ := data["member"].(map[string]any)
	joiner := sanitizeMember(memberRaw, owner.PoolID)
	if joiner == nil || joiner.MemberID != strings.ToLower(stringOf(ticket["from"])) ||
		joiner.PublicKey != stringOf(ticket["fromKey"]) {
		return nil, fail(http.StatusUnauthorized, "POOL_SIGNATURE_INVALID", "The introduction does not match its ticket.")
	}
	opened, err := s.openEnvelope(message.envelope, "introduce", joiner.PublicKey, s.nowMS())
	if err != nil {
		return nil, err
	}
	if opened.From != joiner.MemberID {
		return nil, fail(http.StatusUnauthorized, "POOL_SIGNATURE_INVALID", "The sender does not match.")
	}
	if err := s.assertReachable(ctx, userID, joiner.URL); err != nil {
		return nil, err
	}
	peerKey := stringOf(opened.data()["peerKey"])
	if err := assertPeerKey(peerKey); err != nil {
		return nil, err
	}
	ourKey, err := s.accept(ctx, userID, owner.PoolID, joiner, peerKey)
	if err != nil {
		return nil, err
	}
	if err := s.rememberMember(ctx, userID, owner.PoolID, joiner); err != nil {
		return nil, err
	}
	return s.envelope(ctx, userID, "introduced", map[string]any{
		"pool": owner.PoolID, "peerKey": ourKey,
	}, &joiner.MemberID)
}

// leaveLocally removes everything about this pool here and now: partner keys stop
// working at once, whether or not the partners hear about it.
func (s *Service) leaveLocally(ctx context.Context, userID, poolID string) error {
	owner, err := s.pool(ctx, userID, poolID)
	if err != nil {
		return err
	}
	if owner != nil && owner.IsHost {
		s.hub.closePool(poolID)
	}
	s.stopRelay(userID, poolID)
	links, err := s.links(ctx, userID, poolID)
	if err != nil {
		return err
	}
	for _, link := range links {
		s.teardown(ctx, userID, link)
	}
	return s.deletePool(ctx, userID, poolID)
}

// peerMessage is one message another NeoRouter sent: the signed envelope and its
// body as it arrived, before the signature is checked.
type peerMessage struct {
	envelope  *peerEnvelope
	rawBody   map[string]any
	signature string
}

// unverifiedBody is the envelope body as it arrived.
func (m *peerMessage) unverifiedBody() (map[string]any, error) {
	if m.rawBody == nil {
		return nil, fail(http.StatusBadRequest, "POOL_MESSAGE_INVALID", "The message is malformed.")
	}
	return m.rawBody, nil
}

// unverifiedData is the body's data as it arrived.
func (m *peerMessage) unverifiedData() (map[string]any, error) {
	body, err := m.unverifiedBody()
	if err != nil {
		return nil, err
	}
	data, _ := body["data"].(map[string]any)
	if data == nil {
		data = map[string]any{}
	}
	return data, nil
}

// newPeerMessage decodes one request body into a message.
func newPeerMessage(raw []byte) (*peerMessage, error) {
	value, err := decodeJSON(raw)
	if err != nil {
		return nil, fail(http.StatusBadRequest, "POOL_MESSAGE_INVALID", "The message is malformed.")
	}
	object, _ := value.(map[string]any)
	if object == nil {
		return nil, fail(http.StatusBadRequest, "POOL_MESSAGE_INVALID", "The message is malformed.")
	}
	envelope := &peerEnvelope{}
	signature, _ := object["signature"].(string)
	encoded, err := json.Marshal(object)
	if err != nil {
		return nil, fail(http.StatusBadRequest, "POOL_MESSAGE_INVALID", "The message is malformed.")
	}
	if err := json.Unmarshal(encoded, envelope); err != nil {
		return nil, fail(http.StatusBadRequest, "POOL_MESSAGE_INVALID", "The message is malformed.")
	}
	body, _ := object["body"].(map[string]any)
	return &peerMessage{envelope: envelope, rawBody: body, signature: signature}, nil
}
