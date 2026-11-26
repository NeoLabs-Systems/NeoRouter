package pools

// The pool privacy minimum, the owner's reserve and the day's counters.

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/NeoLabs-Systems/neorouter/internal/providers"
	"github.com/NeoLabs-Systems/neorouter/internal/routerkeys"
)

// TestPoolShieldMinimum checks that secrets are hidden, contact data becomes
// placeholders, and nothing else changes when the shield is off.
func TestPoolShieldMinimum(t *testing.T) {
	off := WithPoolMinimum(PrivacyConfig{
		Enabled: false,
		Modes:   map[string]string{"secret": "off", "card": "off", "email": "off", "person": "off"},
	})
	if !off.Enabled {
		t.Fatal("the pool minimum does not switch the shield on")
	}
	if off.Modes["secret"] != "hide" || off.Modes["card"] != "hide" {
		t.Fatalf("secrets are not hidden: %v", off.Modes)
	}
	if off.Modes["email"] != "restore" {
		t.Fatalf("email is not a placeholder: %v", off.Modes)
	}
	if off.Modes["person"] != "off" {
		t.Fatalf("names were changed with the shield off: %v", off.Modes)
	}
	if string(off.AI) != `{"enabled":false}` {
		t.Fatalf("ai = %s", off.AI)
	}

	on := WithPoolMinimum(PrivacyConfig{
		Enabled:    true,
		Modes:      map[string]string{"secret": "restore", "person": "restore"},
		AI:         json.RawMessage(`{"enabled":true}`),
		AllowTerms: []any{"kept"},
	})
	if on.Modes["secret"] != "hide" {
		t.Fatal("secrets never come back from a partner round trip")
	}
	if on.Modes["person"] != "restore" {
		t.Fatal("own choices above the minimum do not stay")
	}
	if string(on.AI) != `{"enabled":true}` || len(on.AllowTerms) != 1 {
		t.Fatalf("own settings were dropped: %+v", on)
	}
}

// TestPoolPrivacyChoice stores and reads the person's own choice.
func TestPoolPrivacyChoice(t *testing.T) {
	harness := newHarness(t)
	defer harness.close()
	ctx := context.Background()
	alex := harness.register("alex")

	if settings := harness.svc.PoolPrivacySettings(ctx, alex.ID); !settings.KeepSensitivePrivate {
		t.Fatal("the default is not to keep sensitive requests private")
	}
	no := false
	if _, err := harness.svc.SavePoolPrivacy(ctx, alex.ID, PoolPrivacyInput{KeepSensitivePrivate: &no}); err != nil {
		t.Fatalf("save: %v", err)
	}
	if settings := harness.svc.PoolPrivacySettings(ctx, alex.ID); settings.KeepSensitivePrivate {
		t.Fatal("the choice was not stored")
	}
	response := harness.api(alex, "PUT", "/pools/privacy", map[string]any{"keepSensitivePrivate": true})
	if response.Code != 200 {
		t.Fatalf("PUT /pools/privacy: %d %s", response.Code, response.Body.String())
	}
	body := decoded(t, response)
	privacy, _ := body["privacy"].(map[string]any)
	if privacy["keepSensitivePrivate"] != true {
		t.Fatalf("privacy = %v", body["privacy"])
	}
	// A field the schema does not know is refused.
	if response := harness.api(alex, "PUT", "/pools/privacy", map[string]any{"keepSensitivePrivate": true, "extra": 1}); response.Code != 400 {
		t.Fatalf("an unknown field answered %d", response.Code)
	}
}

// TestOwnerReserve checks that the owner keeps what they usually still need today,
// and that a connection they hardly use is almost all for partners.
func TestOwnerReserve(t *testing.T) {
	harness := newHarness(t)
	defer harness.close()
	ctx := context.Background()
	owner := harness.register("owner")
	busy := harness.addAccount(owner, "groq", "busy")
	idle := harness.addAccount(owner, "groq", "idle")

	const day = int64(86_400_000)
	now := (harness.svc.nowMS()/day)*day + 10*3_600_000 // 10:00 UTC today
	insert := `INSERT INTO requests (id, user_id, created_at, api_format, requested_model, account_id, status)
		VALUES (?, ?, ?, 'chat', 'auto', ?, 'success')`
	// Last week, from 10:00 on: 40 requests a day, one day 200 (a spike), and some
	// before 10:00 that do not count.
	for dayIndex := int64(1); dayIndex <= 7; dayIndex++ {
		start := now - dayIndex*day
		count := int64(40)
		if dayIndex == 3 {
			count = 200
		}
		for index := int64(0); index < count; index++ {
			if _, err := harness.db.W.ExecContext(ctx, insert,
				"own-"+itoa(dayIndex)+"-"+itoa(index), owner.ID, start+60_000+index, busy); err != nil {
				t.Fatalf("insert request: %v", err)
			}
		}
		if _, err := harness.db.W.ExecContext(ctx, insert, "early-"+itoa(dayIndex), owner.ID,
			start-3_600_000, busy); err != nil {
			t.Fatalf("insert request: %v", err)
		}
	}
	harness.svc.resetReserveForTests()
	busyModel := Model{AccountID: busy, UserID: owner.ID}
	if expected := harness.svc.expectedOwnUse(ctx, busyModel, now); expected != int64(48) {
		t.Fatalf("a busy day keeps %d, want 48 (40 plus the margin)", expected)
	}
	idleModel := Model{AccountID: idle, UserID: owner.ID}
	if expected := harness.svc.expectedOwnUse(ctx, idleModel, now); expected != 0 {
		t.Fatalf("an unused connection keeps %d", expected)
	}
	quota := func(remaining float64) QuotaState {
		left := remaining
		return QuotaState{Headroom: remaining / 1000, Details: []QuotaDetail{{Metric: "rpd", Remaining: &left}}}
	}
	if !harness.svc.keptForOwner(ctx, busyModel, quota(40), now) {
		t.Fatal("40 left, the owner usually needs 48")
	}
	if harness.svc.keptForOwner(ctx, busyModel, quota(500), now) {
		t.Fatal("500 left is not enough for a partner")
	}
	if harness.svc.keptForOwner(ctx, idleModel, quota(150), now) {
		t.Fatal("an unused connection is not shared")
	}
	if !harness.svc.keptForOwner(ctx, idleModel, quota(50), now) {
		t.Fatal("a small margin always stays")
	}
}

// TestPoolActivity checks the counts the pool page shows, read from request
// metadata only.
func TestPoolActivity(t *testing.T) {
	harness := newHarness(t)
	defer harness.close()
	ctx := context.Background()
	bea := harness.register("bea")
	alex := harness.register("alex")
	created := decoded(t, harness.api(bea, http.MethodPost, "/pools", map[string]any{
		"name": "Friends", "selfName": "bea", "sharedAccountIds": []string{bea.Account},
	}))
	poolID := created["poolId"].(string)
	harness.join(alex, harness.invite(bea, poolID), "alex", []string{alex.Account})
	waitFor(t, "the partners to be linked", 10*time.Second, func() bool {
		return len(partnersOf(t, harness.firstPool(bea))) == 1
	})

	// Bea's side: what she gave to Alex, recorded as requests she served.
	beaLink, err := harness.svc.linkToMember(ctx, bea.ID, poolID, alex.MemberID)
	if err != nil || beaLink == nil {
		t.Fatalf("read link: %v", err)
	}
	keyID := "activity-key"
	if _, err := harness.db.W.ExecContext(ctx, `INSERT INTO router_keys
		(id, user_id, name, prefix, key_hash, mode, peer_link_id) VALUES (?, ?, 'Pool: alex', 'nrt_act',
		'hash-act', 'both', ?)`, keyID, bea.ID, beaLink.ID); err != nil {
		t.Fatalf("create partner key: %v", err)
	}
	now := harness.svc.nowMS()
	for index := 0; index < 3; index++ {
		if _, err := harness.db.W.ExecContext(ctx, `INSERT INTO requests
			(id, user_id, created_at, api_format, requested_model, model_label, account_id, router_key_id,
			 status, input_tokens, output_tokens, cost_usd)
			VALUES (?, ?, ?, 'chat', 'bea/ok-bea', 'ok-bea', ?, ?, 'success', 10, 5, 0.5)`,
			"given-"+itoa(int64(index)), bea.ID, now-int64(index), bea.Account, keyID); err != nil {
			t.Fatalf("insert request: %v", err)
		}
	}
	// Alex's side: what he took from Bea, through her connection.
	alexLink, err := harness.svc.linkToMember(ctx, alex.ID, poolID, bea.MemberID)
	if err != nil || alexLink == nil || alexLink.AccountID == nil {
		t.Fatalf("read Alex's link: %v", err)
	}
	if _, err := harness.db.W.ExecContext(ctx, `INSERT INTO requests
		(id, user_id, created_at, api_format, requested_model, model_label, account_id, status,
		 input_tokens, output_tokens, cost_usd)
		VALUES ('taken-1', ?, ?, 'chat', 'pool-bea/ok-bea', 'ok-bea', ?, 'success', 4, 6, 0)`,
		alex.ID, now, *alexLink.AccountID); err != nil {
		t.Fatalf("insert request: %v", err)
	}

	beaActivity := decoded(t, harness.api(bea, http.MethodGet, "/pools/"+poolID+"/activity?days=30", nil))
	if beaActivity["days"].(float64) != 30 {
		t.Fatalf("days = %v", beaActivity["days"])
	}
	given, _ := beaActivity["given"].(map[string]any)
	if given["requests"].(float64) != 3 || given["tokens"].(float64) != 45 {
		t.Fatalf("given = %v", given)
	}
	byPartner, _ := given["byPartner"].([]any)
	first, _ := byPartner[0].(map[string]any)
	if first["name"] != "alex" {
		t.Fatalf("byPartner = %v", byPartner)
	}
	byConnection, _ := given["byConnection"].([]any)
	connection, _ := byConnection[0].(map[string]any)
	if connection["key"] != bea.Account {
		t.Fatalf("byConnection = %v", byConnection)
	}

	alexActivity := decoded(t, harness.api(alex, http.MethodGet, "/pools/"+poolID+"/activity", nil))
	if alexActivity["days"].(float64) != 7 {
		t.Fatalf("an unknown range falls back to a week: %v", alexActivity["days"])
	}
	taken, _ := alexActivity["taken"].(map[string]any)
	if taken["requests"].(float64) != 1 {
		t.Fatalf("taken = %v", taken)
	}
	if response := harness.api(alex, http.MethodGet, "/pools/00000000-0000-0000-0000-000000000000/activity", nil); response.Code != 404 {
		t.Fatalf("an unknown pool answered %d", response.Code)
	}
}

// TestTrafficCounters checks the per-day counters both sides agree on.
func TestTrafficCounters(t *testing.T) {
	harness := newHarness(t)
	defer harness.close()
	ctx := context.Background()
	owner := harness.register("owner")
	harness.linkForTests("link-1", owner.ID, "p1", "m", "P", "https://p.example")
	now := harness.svc.nowMS()
	if err := harness.svc.trafficAdd(ctx, "link-1", "given", 2, 100, now); err != nil {
		t.Fatalf("count: %v", err)
	}
	if err := harness.svc.trafficAdd(ctx, "link-1", "taken", 1, 40, now); err != nil {
		t.Fatalf("count: %v", err)
	}
	today, err := harness.svc.trafficToday(ctx, "link-1", "given", now)
	if err != nil || today.Requests != 2 || today.Tokens != 100 {
		t.Fatalf("today = %+v (%v)", today, err)
	}
	window, err := harness.svc.trafficWindow(ctx, "link-1", 7, now)
	if err != nil || window.Given.Requests != 2 || window.Taken.Tokens != 40 {
		t.Fatalf("window = %+v (%v)", window, err)
	}
	if reset := trafficNextReset(now); reset <= now || reset%86_400_000 != 0 {
		t.Fatalf("reset = %d", reset)
	}
}

// TestPoolKeyUse checks the pool-facing helpers the gateway reads: a pool key may
// use pools, a partner key never, and a paused pool shares with nobody.
func TestPoolKeyUse(t *testing.T) {
	harness := newHarness(t)
	defer harness.close()
	ctx := context.Background()
	alex := harness.register("alex")
	bea := harness.register("bea")
	created := decoded(t, harness.api(alex, http.MethodPost, "/pools", map[string]any{
		"name": "Friends", "selfName": "alex", "sharedAccountIds": []string{alex.Account},
	}))
	poolID := created["poolId"].(string)
	harness.join(bea, harness.invite(alex, poolID), "bea", []string{bea.Account})

	ownKey := &routerkeys.Row{ID: "own", UserID: alex.ID, PoolAccess: "own"}
	poolKey := &routerkeys.Row{ID: "pool", UserID: alex.ID, PoolAccess: "pool"}
	linkID := "link-for-key"
	partnerKey := &routerkeys.Row{ID: "partner", UserID: alex.ID, PoolAccess: "pool", PeerLinkID: &linkID}
	if harness.svc.MayUsePool(ctx, ownKey) {
		t.Fatal("an own key may use pools")
	}
	if !harness.svc.MayUsePool(ctx, poolKey) {
		t.Fatal("a pool key may not use pools")
	}
	if harness.svc.MayUsePool(ctx, partnerKey) {
		t.Fatal("a partner key may use pools")
	}

	alexSide, err := harness.svc.linkToMember(ctx, alex.ID, poolID, bea.MemberID)
	if err != nil || alexSide == nil {
		t.Fatalf("read alex's link: %v", err)
	}
	key := harness.partnerKey(t, alex, alexSide.ID, "bea")
	if !harness.svc.SharingWith(ctx, key) {
		t.Fatal("a partner key does not share with a fresh pool")
	}
	if poolIDOf := harness.svc.PoolOfKey(ctx, key); poolIDOf != poolID {
		t.Fatalf("PoolOfKey = %q", poolIDOf)
	}
	if paused := harness.api(alex, http.MethodPut, "/pools/"+poolID+"/paused", map[string]any{"paused": true}); paused.Code != 200 {
		t.Fatalf("pause: %d", paused.Code)
	}
	if harness.svc.SharingWith(ctx, key) {
		t.Fatal("a paused pool still shares")
	}
}

// TestRelayTransportRefusesAnUnknownPool checks the error the HTTP layer reads as
// "could not reach the partner".
func TestRelayTransportRefusesAnUnknownPool(t *testing.T) {
	harness := newHarness(t)
	defer harness.close()
	alex := harness.register("alex")
	request, err := http.NewRequest(http.MethodGet, "relay://11111111-1111-4111-8111-111111111111."+
		"22222222-2222-4222-8222-222222222222/v1/models", nil)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	_, err = harness.svc.RelayTransport(context.Background(), request.URL.String(), request,
		providers.TransportInfo{Owner: alex.ID})
	var poolErr *HttpError
	if err == nil || !errors.As(err, &poolErr) || poolErr.Code != "RELAY_UNREACHABLE" {
		t.Fatalf("a relay address in a pool we are not in: %v", err)
	}
}
