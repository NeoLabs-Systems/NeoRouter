package pools

// The share policy's unit tests: fair automatic caps, safe bounds, the kinds
// each connection shares, and admission.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"testing"
	"time"

	"github.com/NeoLabs-Systems/neorouter/internal/routerkeys"
)

// TestAutomaticCapsAreFair checks that every partner starts with the same base, a
// partner who gives gets more, and one who only takes gets less.
func TestAutomaticCapsAreFair(t *testing.T) {
	harness := newHarness(t)
	defer harness.close()
	ctx := context.Background()
	owner := harness.register("owner")
	harness.linkForTests("l1", owner.ID, "p1", "m", "P", "https://p.example")
	harness.linkForTests("l2", owner.ID, "p1", "m2", "Q", "https://q.example")
	now := harness.svc.nowMS()

	settings := shareSettingsOf(nil)
	if balance := harness.svc.balanceWith(ctx, "l1", now); balance.State != "new" {
		t.Fatalf("a new partner's state = %s", balance.State)
	}
	if caps := harness.svc.capsFor(ctx, "l1", settings, now); caps.Requests.float() != float64(AutoShare.RequestsPerDay) {
		t.Fatalf("a new partner gets %v requests", caps.Requests)
	}
	// l1 only takes from us: down to half, never lower.
	if err := harness.svc.trafficAdd(ctx, "l1", "given", 5000, 0, now); err != nil {
		t.Fatalf("count traffic: %v", err)
	}
	if factor := harness.svc.balanceWith(ctx, "l1", now).Factor; factor != Balance.Floor {
		t.Fatalf("a partner who only takes has factor %v", factor)
	}
	if caps := harness.svc.capsFor(ctx, "l1", settings, now); caps.Requests.float() !=
		math.Floor(float64(AutoShare.RequestsPerDay)*Balance.Floor) {
		t.Fatalf("a partner who only takes gets %v requests", caps.Requests)
	}
	// l2 gives us far more than it takes: up to three times the base.
	if err := harness.svc.trafficAdd(ctx, "l2", "taken", 900, 0, now); err != nil {
		t.Fatalf("count traffic: %v", err)
	}
	if err := harness.svc.trafficAdd(ctx, "l2", "given", 100, 0, now); err != nil {
		t.Fatalf("count traffic: %v", err)
	}
	if factor := harness.svc.balanceWith(ctx, "l2", now).Factor; factor != Balance.Ceiling {
		t.Fatalf("a partner who gives has factor %v", factor)
	}
	if caps := harness.svc.capsFor(ctx, "l2", settings, now); caps.Requests.float() !=
		float64(AutoShare.RequestsPerDay)*Balance.Ceiling {
		t.Fatalf("a partner who gives gets %v requests", caps.Requests)
	}
	// A value the owner sets replaces the automatic one, whatever the balance.
	fixed := harness.svc.capsFor(ctx, "l1", shareSettingsOf(&pool{Share: map[string]any{"requestsPerDay": float64(1000)}}), now)
	if fixed.Requests.float() != 1000 {
		t.Fatalf("a fixed cap = %v", fixed.Requests)
	}
	if fixed.Auto["requestsPerDay"] {
		t.Fatal("a fixed cap still counts as automatic")
	}
	if !fixed.Auto["tokensPerDay"] {
		t.Fatal("an untouched cap is not automatic")
	}
}

// linkForTests inserts one partner link.
func (h *harness) linkForTests(id, userID, poolID, memberID, name, url string) {
	h.t.Helper()
	if _, err := h.db.W.ExecContext(context.Background(), `INSERT INTO peer_links
		(id, user_id, pool_id, member_id, name, url, public_key) VALUES (?, ?, ?, ?, ?, ?, 'k')`,
		id, userID, poolID, memberID, name, url); err != nil {
		h.t.Fatalf("create partner link: %v", err)
	}
}

// TestShareSettingsBounds checks that set values are clamped to safe bounds and
// missing ones stay automatic.
func TestShareSettingsBounds(t *testing.T) {
	settings := shareSettingsOf(&pool{Share: map[string]any{
		"requestsPerDay": float64(-5), "concurrent": float64(999), "reserve": "x",
	}})
	if settings.RequestsPerDay == nil || settings.RequestsPerDay.float() != 1 {
		t.Fatalf("requestsPerDay = %v", settings.RequestsPerDay)
	}
	if settings.Concurrent == nil || settings.Concurrent.float() != 20 {
		t.Fatalf("concurrent = %v", settings.Concurrent)
	}
	if settings.Reserve != nil {
		t.Fatalf("an unusable reserve stays set: %v", settings.Reserve)
	}
	if settings.TokensPerDay != nil {
		t.Fatalf("a missing cap is set: %v", settings.TokensPerDay)
	}
	encoded, err := json.Marshal(settings)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	if string(encoded) != `{"requestsPerDay":1,"tokensPerDay":null,"concurrent":20,"reserve":null}` {
		t.Fatalf("settings JSON = %s", encoded)
	}
}

// TestPoolSharing covers what is shared with a pool: the kinds ticked for each
// connection, and that an unshared or a used-up share is refused.
func TestPoolSharing(t *testing.T) {
	harness := newHarness(t)
	defer harness.close()
	ctx := context.Background()
	bea := harness.register("bea")
	alex := harness.register("alex")
	created := decoded(t, harness.api(bea, http.MethodPost, "/pools", map[string]any{
		"name": "Friends", "selfName": "bea", "sharedAccountIds": []string{bea.Account},
	}))
	poolID := created["poolId"].(string)
	now := harness.svc.nowMS()

	// A free model on the shared connection is offered; one on another connection is
	// not.
	freeRef := harness.addModel(bea.Account, "ok-bea", true)
	otherAccount := harness.addAccount(bea, "custom_openai", "other")
	harness.addModel(otherAccount, "private-bea", true)
	shared := Model{Ref: freeRef, AccountID: bea.Account, UserID: bea.ID, DefinitionID: "custom_openai", IsFree: true}
	if !harness.svc.Offers(ctx, poolID, shared, now) {
		t.Fatal("a shared connection's free model is not offered")
	}
	unshared := Model{Ref: freeRef, AccountID: otherAccount, UserID: bea.ID, DefinitionID: "custom_openai", IsFree: true}
	if harness.svc.Offers(ctx, poolID, unshared, now) {
		t.Fatal("an unshared connection's model is offered")
	}
	// A partner's own connection is never passed on.
	peer := Model{Ref: freeRef, AccountID: otherAccount, UserID: bea.ID, DefinitionID: PeerDefinition, IsFree: true}
	if reason := harness.svc.Rejection(ctx, peer, RejectOptions{PoolID: poolID}, time.UnixMilli(now)); reason != "peer_no_forward" {
		t.Fatalf("a partner's connection answered %q", reason)
	}
	if reason := harness.svc.Rejection(ctx, unshared, RejectOptions{PoolID: poolID}, time.UnixMilli(now)); reason != "not_shared" {
		t.Fatalf("an unshared model answered %q", reason)
	}
	if reason := harness.svc.Rejection(ctx, shared, RejectOptions{PoolID: poolID}, time.UnixMilli(now)); reason != "" {
		t.Fatalf("a shared model answered %q", reason)
	}

	// The kinds ticked for it: free only at first, then paid as well.
	kinds := func() []string {
		return harness.svc.shareKindsInPool(ctx, poolID, bea.Account, now)
	}
	if fmt.Sprint(kinds()) != "[free]" {
		t.Fatalf("a new share offers %v", kinds())
	}
	harness.mustShare(t, bea, poolID, bea.Account, map[string]any{"kinds": []string{"free", "paid"}})
	harness.svc.shareMu.Lock()
	delete(harness.svc.kindsCache, poolID+"|"+bea.Account)
	harness.svc.shareMu.Unlock()
	if fmt.Sprint(kinds()) != "[free paid]" {
		t.Fatalf("free and paid offers %v", kinds())
	}
	paidModel := Model{Ref: freeRef, AccountID: bea.Account, UserID: bea.ID, DefinitionID: "custom_openai"}
	if !harness.svc.Offers(ctx, poolID, paidModel, now) {
		t.Fatal("a paid model is not offered once paid is ticked")
	}
	harness.mustShare(t, bea, poolID, bea.Account, map[string]any{"shared": false})
	if harness.svc.Offers(ctx, poolID, shared, now) {
		t.Fatal("an unshared connection is still offered")
	}
	_ = alex
}

// mustShare changes one connection's share and checks the answer.
func (h *harness) mustShare(t *testing.T, who *person, poolID, accountID string, body map[string]any) {
	t.Helper()
	response := h.api(who, http.MethodPut, "/pools/"+poolID+"/accounts/"+accountID+"/share", body)
	if response.Code != http.StatusOK {
		t.Fatalf("share %s: %d %s", accountID, response.Code, response.Body.String())
	}
}

// TestAdmission covers what one partner request may use, and how it is counted.
func TestAdmission(t *testing.T) {
	harness := newHarness(t)
	defer harness.close()
	ctx := context.Background()
	bea := harness.register("bea")
	created := decoded(t, harness.api(bea, http.MethodPost, "/pools", map[string]any{
		"name": "Friends", "selfName": "bea", "sharedAccountIds": []string{bea.Account},
	}))
	poolID := created["poolId"].(string)

	// The partner's link and the key it calls us with.
	link := harness.svc.mustInsertLink(t, bea.ID, poolID, member{MemberID: "11111111-1111-4111-8111-111111111111",
		Name: "alex", URL: "https://alex.example.com", PublicKey: "pk"})
	keyID := "admit-key"
	if _, err := harness.db.W.ExecContext(ctx, `INSERT INTO router_keys
		(id, user_id, name, prefix, key_hash, mode, pool_access, peer_link_id)
		VALUES (?, ?, 'Pool: alex', 'nrt_admit', 'hash-admit', 'both', 'own', ?)`, keyID, bea.ID, link.ID); err != nil {
		t.Fatalf("create partner key: %v", err)
	}
	key := &routerkeys.Row{ID: keyID, UserID: bea.ID, PeerLinkID: &link.ID}
	now := harness.svc.nowMS()

	admit, err := harness.svc.Admit(ctx, key, now)
	if err != nil {
		t.Fatalf("admit: %v", err)
	}
	if admit.Headers.Get("x-ratelimit-limit-requests-day") != "300" {
		t.Fatalf("limit header = %q", admit.Headers.Get("x-ratelimit-limit-requests-day"))
	}
	if admit.Headers.Get("x-ratelimit-remaining-requests-day") != "299" {
		t.Fatalf("remaining header = %q", admit.Headers.Get("x-ratelimit-remaining-requests-day"))
	}
	admit.Release(true)
	// A refused request is not counted against the day's share.
	refused, err := harness.svc.Admit(ctx, key, now)
	if err != nil {
		t.Fatalf("admit: %v", err)
	}
	refused.Release(false)
	used, err := harness.svc.trafficToday(ctx, link.ID, "given", now)
	if err != nil {
		t.Fatalf("read traffic: %v", err)
	}
	if used.Requests != 1 {
		t.Fatalf("given requests = %d, want 1", used.Requests)
	}

	// The owner's fixed cap is what a partner gets: past it, the day's share is used
	// up.
	if _, err := harness.db.W.ExecContext(ctx, `UPDATE pools SET share_json = '{"requestsPerDay":1}' WHERE user_id = ? AND pool_id = ?`,
		bea.ID, poolID); err != nil {
		t.Fatalf("fix the cap: %v", err)
	}
	_, err = harness.svc.Admit(ctx, key, now)
	var poolErr *HttpError
	if !errors.As(err, &poolErr) || poolErr.Code != "POOL_SHARE_USED_UP" || poolErr.Status != http.StatusTooManyRequests {
		t.Fatalf("past the cap: %v", err)
	}
	details, _ := poolErr.Details.(map[string]any)
	if details["retryAfterSeconds"] == nil {
		t.Fatalf("no retry hint: %v", poolErr.Details)
	}

	// Paused: the partner's router retries later rather than giving up.
	if _, err := harness.db.W.ExecContext(ctx, `UPDATE pools SET share_json = '{}', paused = 1 WHERE user_id = ? AND pool_id = ?`,
		bea.ID, poolID); err != nil {
		t.Fatalf("pause the pool: %v", err)
	}
	_, err = harness.svc.Admit(ctx, key, now)
	if !errors.As(err, &poolErr) || poolErr.Code != "POOL_PAUSED" || poolErr.Status != http.StatusServiceUnavailable {
		t.Fatalf("paused: %v", err)
	}

	// A key whose link is gone shares nothing.
	gone := "gone-link"
	orphan := &routerkeys.Row{ID: keyID, UserID: bea.ID, PeerLinkID: &gone}
	_, err = harness.svc.Admit(ctx, orphan, now)
	if !errors.As(err, &poolErr) || poolErr.Code != "POOL_NOT_SHARING" {
		t.Fatalf("a key without a link: %v", err)
	}
}

// insertLinkForTests inserts one link and returns it.
func (s *Service) mustInsertLink(t *testing.T, userID, poolID string, value member) *peerLink {
	t.Helper()
	link, err := s.insertLink(context.Background(), userID, poolID, value)
	if err != nil {
		t.Fatalf("insert link: %v", err)
	}
	return link
}
