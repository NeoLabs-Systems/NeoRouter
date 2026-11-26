package pools

// What a partner may take from us, and when (share_policy.js).
//
//  1. Own requests first: a connection or model takes partner work only while it
//     keeps what its owner is expected to need today (owner_reserve.go), and
//     partner keys have the lowest priority, so they yield whenever the owner
//     waits for the same capacity.
//  2. Daily caps per partner, automatic and fair: every partner starts with the
//     same share (AutoShare); over the last week, one who gives us more gets up
//     to three times that, one who only takes down to half. Never a hard stop.
//  3. Fixed limits: the owner may set any cap by hand; a set value replaces the
//     automatic one for that cap.
//
// Only models on connections the owner chose to share are offered, and only the
// kinds chosen for that connection in that pool: free (the default), plan and
// paid, each on its own.

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"strconv"
	"time"

	"github.com/NeoLabs-Systems/neorouter/internal/routerkeys"
)

// AutoShare is the base every partner starts with (AUTO_SHARE).
var AutoShare = struct {
	RequestsPerDay int64
	TokensPerDay   int64
	Concurrent     int64
}{
	RequestsPerDay: 300,
	TokensPerDay:   600_000,
	Concurrent:     2,
}

// shareFields lists the share settings in the JS object's key order.
var shareFields = []string{"requestsPerDay", "tokensPerDay", "concurrent", "reserve"}

// shareBounds are the safe range of each fixed value.
var shareBounds = map[string][2]float64{
	"requestsPerDay": {1, 100_000},
	"tokensPerDay":   {1_000, 100_000_000},
	"concurrent":     {1, 20},
	"reserve":        {0, 0.9},
}

// ShareKinds are the kinds of model a shared connection can give a pool.
var ShareKinds = []string{"free", "plan", "paid"}

// ShareBounds is the safe range of one fixed share value (for the route's
// validation).
func ShareBounds(field string) (min, max float64, ok bool) {
	bounds, ok := shareBounds[field]
	return bounds[0], bounds[1], ok
}

// Balance tunes the automatic share: the smoothing keeps a few requests from
// swinging it; a new partner starts at 1.
var Balance = struct {
	Days      int
	Smoothing float64
	Floor     float64
	Ceiling   float64
}{
	Days: 7, Smoothing: 100, Floor: 0.5, Ceiling: 3,
}

// busyRetrySeconds is how long a partner waits when too many requests to it run
// at once.
const busyRetrySeconds = 2

// sharedTTL is how long one pool's share kinds are reused.
const sharedTTL = 5000

// jsNum is a number that renders the way JSON.stringify renders it.
type jsNum float64

// MarshalJSON prints the number without a needless fraction or exponent.
func (n jsNum) MarshalJSON() ([]byte, error) {
	return []byte(jsNumber(strconv.FormatFloat(float64(n), 'g', -1, 64))), nil
}

// float is the number as a float64.
func (n jsNum) float() float64 { return float64(n) }

// numPtr is a number pointer, nil when the value is automatic (JSON null).
func numPtr(value float64) *jsNum {
	number := jsNum(value)
	return &number
}

// ShareSettings is the owner's fixed share values, nil where the value is
// automatic.
type ShareSettings struct {
	RequestsPerDay *jsNum `json:"requestsPerDay"`
	TokensPerDay   *jsNum `json:"tokensPerDay"`
	Concurrent     *jsNum `json:"concurrent"`
	Reserve        *jsNum `json:"reserve"`
}

// field reads one setting; ok is false when it is automatic.
func (s ShareSettings) field(name string) (float64, bool) {
	var value *jsNum
	switch name {
	case "requestsPerDay":
		value = s.RequestsPerDay
	case "tokensPerDay":
		value = s.TokensPerDay
	case "concurrent":
		value = s.Concurrent
	case "reserve":
		value = s.Reserve
	}
	if value == nil {
		return 0, false
	}
	return value.float(), true
}

// shareSettingsOf clamps the owner's fixed values to safe bounds; a missing or
// unusable one stays automatic.
func shareSettingsOf(value *pool) ShareSettings {
	stored := map[string]any{}
	if value != nil && value.Share != nil {
		stored = value.Share
	}
	settings := ShareSettings{}
	for _, name := range shareFields {
		bounds := shareBounds[name]
		raw, present := stored[name]
		if !present || raw == nil {
			continue
		}
		number := math.NaN()
		switch typed := raw.(type) {
		case float64:
			number = typed
		case int:
			number = float64(typed)
		case int64:
			number = float64(typed)
		case string:
			if typed == "" {
				continue
			}
			parsed, err := strconv.ParseFloat(typed, 64)
			if err != nil {
				continue
			}
			number = parsed
		}
		if math.IsNaN(number) || math.IsInf(number, 0) {
			continue
		}
		clamped := numPtr(finiteOrNaN(math.Min(bounds[1], math.Max(bounds[0], number))))
		switch name {
		case "requestsPerDay":
			settings.RequestsPerDay = clamped
		case "tokensPerDay":
			settings.TokensPerDay = clamped
		case "concurrent":
			settings.Concurrent = clamped
		case "reserve":
			settings.Reserve = clamped
		}
	}
	return settings
}

// finiteOrNaN keeps a finite number and turns anything else into NaN, which the
// JSON encoder reports as automatic.
func finiteOrNaN(value float64) float64 {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return math.NaN()
	}
	return value
}

// balanceSide is what one direction used over the window.
type balanceSide struct {
	Requests int64 `json:"requests"`
	Tokens   int64 `json:"tokens"`
}

// ShareBalance is give and take with one partner over the last week.
type ShareBalance struct {
	Given  balanceSide `json:"given"`
	Taken  balanceSide `json:"taken"`
	Factor float64     `json:"factor"`
	State  string      `json:"state"`
}

// balanceWith is give and take with one partner over the last week, from our
// side. factor: how much of the automatic share they get (1 = the base share).
// state: new | balanced | giving_more (we give more) | taking_more (we take more).
func (s *Service) balanceWith(ctx context.Context, linkID string, now int64) ShareBalance {
	window, _ := s.trafficWindow(ctx, linkID, Balance.Days, now)
	given, taken := window.Given, window.Taken
	ratio := float64(given.Requests) / math.Max(float64(taken.Requests), 1)
	raw := (float64(taken.Requests) + Balance.Smoothing) / (float64(given.Requests) + Balance.Smoothing)
	factor := math.Min(Balance.Ceiling, math.Max(Balance.Floor, raw))
	state := "balanced"
	switch {
	case given.Requests+taken.Requests < 10:
		state = "new"
	case ratio > 2:
		state = "giving_more"
	case float64(taken.Requests) > 2*float64(given.Requests):
		state = "taking_more"
	}
	return ShareBalance{Given: given, Taken: taken, Factor: math.Round(factor*1000) / 1000, State: state}
}

// Caps is today's allowance for one partner; Auto marks which caps are
// automatic.
type Caps struct {
	Requests   jsNum           `json:"requests"`
	Tokens     jsNum           `json:"tokens"`
	Concurrent jsNum           `json:"concurrent"`
	Reserve    *jsNum          `json:"reserve"`
	Factor     float64         `json:"factor"`
	Auto       map[string]bool `json:"auto"`
}

// capsFor is today's caps for one partner: automatic (the base share times the
// balance factor) unless the owner set a value.
func (s *Service) capsFor(ctx context.Context, linkID string, settings ShareSettings, now int64) Caps {
	factor := s.balanceWith(ctx, linkID, now).Factor
	caps := Caps{Factor: factor, Auto: map[string]bool{}}
	for _, name := range shareFields {
		_, fixed := settings.field(name)
		caps.Auto[name] = !fixed
	}
	if requests, fixed := settings.field("requestsPerDay"); fixed {
		caps.Requests = jsNum(requests)
	} else {
		caps.Requests = jsNum(math.Max(1, math.Floor(float64(AutoShare.RequestsPerDay)*factor)))
	}
	if tokens, fixed := settings.field("tokensPerDay"); fixed {
		caps.Tokens = jsNum(tokens)
	} else {
		caps.Tokens = jsNum(math.Max(1, math.Floor(float64(AutoShare.TokensPerDay)*factor)))
	}
	if concurrent, fixed := settings.field("concurrent"); fixed {
		caps.Concurrent = jsNum(concurrent)
	} else {
		caps.Concurrent = jsNum(math.Min(4, math.Max(1, math.Round(float64(AutoShare.Concurrent)*factor))))
	}
	if reserve, fixed := settings.field("reserve"); fixed {
		caps.Reserve = numPtr(reserve)
	}
	return caps
}

// Admission is what one partner request may use, and how to end it.
type Admission struct {
	// Reserve is the share of each allowance the owner keeps by hand, nil for the
	// automatic reserve.
	Reserve *jsNum
	// PoolID is the pool the partner key belongs to.
	PoolID  string
	Headers http.Header

	service  *Service
	linkID   string
	released bool
}

// Release ends the request. served false means nothing here could answer it, so
// it is not counted against the day's share.
func (a *Admission) Release(served bool) {
	if a == nil {
		return
	}
	a.service.shareMu.Lock()
	if a.released {
		a.service.shareMu.Unlock()
		return
	}
	a.released = true
	a.service.inflight[a.linkID] = maxInt64(0, a.service.inflight[a.linkID]-1)
	a.service.shareMu.Unlock()
	if !served {
		if err := a.service.trafficAdd(context.Background(), a.linkID, "given", -1, 0, a.service.nowMS()); err != nil {
			a.service.log().Warn("Could not uncount a refused partner request", "error", err)
		}
	}
}

// Admit lets one partner request in, or refuses it (429 when the day's share is
// used up or too many run at once). It returns the rate-limit headers for the
// partner's router and a release for when the request is done.
func (s *Service) Admit(ctx context.Context, key *routerkeys.Row, now int64) (*Admission, error) {
	var link *peerLink
	if key != nil && key.PeerLinkID != nil {
		found, err := s.linkByID(ctx, *key.PeerLinkID)
		if err != nil {
			return nil, err
		}
		link = found
	}
	var owner *pool
	if link != nil {
		found, err := s.pool(ctx, key.UserID, link.PoolID)
		if err != nil {
			return nil, err
		}
		owner = found
	}
	if link == nil || owner == nil {
		return nil, fail(http.StatusForbidden, "POOL_NOT_SHARING", "This partner is not sharing with you.")
	}
	// Paused for now: temporary, so the partner's router retries later rather than
	// giving up.
	if owner.Paused || link.Status != "active" {
		paused := fail(http.StatusServiceUnavailable, "POOL_PAUSED", "This partner paused sharing for now.")
		paused.Details = map[string]any{"retryAfterSeconds": 300}
		return nil, paused
	}
	settings := shareSettingsOf(owner)
	caps := s.capsFor(ctx, link.ID, settings, now)
	used, err := s.trafficToday(ctx, link.ID, "given", now)
	if err != nil {
		return nil, err
	}
	resetSeconds := maxInt64(1, int64(math.Ceil(float64(trafficNextReset(now)-now)/1000)))
	if float64(used.Requests) >= caps.Requests.float() || float64(used.Tokens) >= caps.Tokens.float() {
		usedUp := fail(http.StatusTooManyRequests, "POOL_SHARE_USED_UP", "Today’s share from this partner is used up.")
		usedUp.Details = map[string]any{"retryAfterSeconds": resetSeconds}
		return nil, usedUp
	}
	s.shareMu.Lock()
	running := s.inflight[link.ID]
	if float64(running) >= caps.Concurrent.float() {
		s.shareMu.Unlock()
		busy := fail(http.StatusTooManyRequests, "POOL_SHARE_BUSY", "Too many requests to this partner at once.")
		busy.Details = map[string]any{"retryAfterSeconds": busyRetrySeconds}
		return nil, busy
	}
	s.inflight[link.ID] = running + 1
	s.shareMu.Unlock()
	if err := s.trafficAdd(ctx, link.ID, "given", 1, 0, now); err != nil {
		return nil, err
	}
	headers := http.Header{}
	headers.Set("x-ratelimit-limit-requests-day", numberText(caps.Requests.float()))
	headers.Set("x-ratelimit-remaining-requests-day", numberText(math.Max(0, caps.Requests.float()-float64(used.Requests)-1)))
	headers.Set("x-ratelimit-reset-requests-day", fmt.Sprintf("%ds", resetSeconds))
	headers.Set("x-ratelimit-limit-tokens-day", numberText(caps.Tokens.float()))
	headers.Set("x-ratelimit-remaining-tokens-day", numberText(math.Max(0, caps.Tokens.float()-float64(used.Tokens))))
	headers.Set("x-ratelimit-reset-tokens-day", fmt.Sprintf("%ds", resetSeconds))
	return &Admission{Reserve: caps.Reserve, PoolID: link.PoolID, Headers: headers, service: s, linkID: link.ID}, nil
}

// numberText renders a whole number the way String(n) does.
func numberText(value float64) string {
	return jsNumber(strconv.FormatFloat(value, 'g', -1, 64))
}

// shareKindsEntry is one cached answer of shareKindsInPool.
type shareKindsEntry struct {
	at    int64
	kinds []string
}

// shareKindsInPool is the kinds a connection shares in a pool, nil when it is
// not shared there (each pool has its own list).
func (s *Service) shareKindsInPool(ctx context.Context, poolID, accountID string, now int64) []string {
	id := poolID + "|" + accountID
	s.shareMu.Lock()
	hit, ok := s.kindsCache[id]
	s.shareMu.Unlock()
	if ok && now-hit.at < sharedTTL {
		return hit.kinds
	}
	value, err := s.shareKindsOfRow(ctx, poolID, accountID)
	if err != nil {
		s.log().Warn("Could not read a pool's share kinds", "error", err)
	}
	s.shareMu.Lock()
	if len(s.kindsCache) > 5000 {
		s.kindsCache = map[string]shareKindsEntry{}
	}
	s.kindsCache[id] = shareKindsEntry{at: now, kinds: value}
	s.shareMu.Unlock()
	return value
}

// kindOf is the kind of model one entry is.
func kindOf(model Model) string {
	if model.IsFree {
		return "free"
	}
	if model.IsPlan {
		return "plan"
	}
	return "paid"
}

// Offers reports whether partners in this pool are offered this model at all.
func (s *Service) Offers(ctx context.Context, poolID string, model Model, now int64) bool {
	if poolID == "" || model.IsPeer() {
		return false
	}
	return containsString(s.shareKindsInPool(ctx, poolID, model.AccountID, now), kindOf(model))
}

// PoolOfKey is the pool a partner key belongs to, "" when its link is gone.
func (s *Service) PoolOfKey(ctx context.Context, key *routerkeys.Row) string {
	if key == nil || key.PeerLinkID == nil {
		return ""
	}
	link, err := s.linkByID(ctx, *key.PeerLinkID)
	if err != nil || link == nil {
		return ""
	}
	return link.PoolID
}

// RejectOptions are what the router knows about a request when it asks whether a
// partner may take it.
type RejectOptions struct {
	InputTokens float64
	// Reserve is the share of each allowance the owner fixed by hand, nil for the
	// automatic reserve.
	Reserve *float64
	PoolID  string
}

// quotaStateOf reads one model's quota picture; a failure counts as "cannot
// tell", so a database hiccup does not stop sharing.
func (s *Service) quotaStateOf(model Model, inputTokens float64, now time.Time) QuotaState {
	if s.Quota == nil {
		return QuotaState{Headroom: 1}
	}
	state, err := s.Quota(model, inputTokens, now)
	if err != nil {
		s.log().Warn("Could not read a model's quota for a pool request", "error", err)
		return QuotaState{Headroom: 1}
	}
	return state
}

// Rejection is why this model may not serve a request that came in through a
// partner's key (or ""). Partner work never goes on to another partner.
func (s *Service) Rejection(ctx context.Context, model Model, opts RejectOptions, now time.Time) string {
	if model.IsPeer() {
		return "peer_no_forward"
	}
	if !s.Offers(ctx, opts.PoolID, model, now.UnixMilli()) {
		return "not_shared"
	}
	state := s.quotaStateOf(model, opts.InputTokens, now)
	if opts.Reserve != nil {
		if state.Headroom < *opts.Reserve {
			return "kept_for_owner"
		}
	} else if s.keptForOwner(ctx, model, state, now.UnixMilli()) {
		return "kept_for_owner"
	}
	// Parallel slots: the fixed share, or else one slot always stays free for the
	// owner.
	if s.Concurrency != nil {
		if limit, known := s.Concurrency(model, now); known && limit > 0 {
			allowed := maxInt64(1, int64(limit)-1)
			if opts.Reserve != nil {
				allowed = maxInt64(1, int64(math.Floor(float64(limit)*(1-*opts.Reserve))))
			}
			if int64(s.inflightOf(model)) >= allowed {
				return "kept_for_owner"
			}
		}
	}
	return ""
}

// inflightOf is how many requests are running on a model's account.
func (s *Service) inflightOf(model Model) int {
	if s.Inflight == nil {
		return 0
	}
	return s.Inflight(model)
}

// SharingWith reports whether this partner key gets anything right now (pool and
// link not paused).
func (s *Service) SharingWith(ctx context.Context, key *routerkeys.Row) bool {
	if key == nil || key.PeerLinkID == nil {
		return false
	}
	link, err := s.linkByID(ctx, *key.PeerLinkID)
	if err != nil || link == nil {
		return false
	}
	owner, err := s.pool(ctx, key.UserID, link.PoolID)
	if err != nil || owner == nil {
		return false
	}
	return !owner.Paused && link.Status == "active"
}

// SetAccountShare shares one connection in one pool, reporting false for a
// connection that is not one's own.
func (s *Service) SetAccountShare(ctx context.Context, userID, poolID, accountID string, change shareChange) (bool, error) {
	var one int
	err := s.DB.R.QueryRowContext(ctx, `SELECT 1 FROM provider_accounts WHERE id = ? AND user_id = ?
		AND definition_id != ?`, accountID, userID, PeerDefinition).Scan(&one)
	if err != nil {
		if isNoRows(err) {
			return false, nil
		}
		return false, fmt.Errorf("pools: check the connection: %w", err)
	}
	if err := s.setShare(ctx, userID, poolID, accountID, change); err != nil {
		return false, err
	}
	s.shareMu.Lock()
	delete(s.kindsCache, poolID+"|"+accountID)
	s.shareMu.Unlock()
	return true, nil
}

// forgetShare forgets a link's in-flight count.
func (s *Service) forgetShare(linkID string) {
	s.shareMu.Lock()
	delete(s.inflight, linkID)
	s.shareMu.Unlock()
}

func maxInt64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}
