package pools

// Pools between NeoRouters (test/api/pools.test.js). Three people (Alex, Bea, Cem)
// take part from one test server: each has their own space, signing key and
// connection, and every pool call travels over real HTTP to the server's own
// address, exactly as it would between three machines.
//
// The JS suite also drives chat completions and the keys/models pages, which are
// other domains; the pool's side of those cases is here (what is shared, what a
// partner may use, what is counted) and the gateway's part is covered by its own
// package.

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/NeoLabs-Systems/neorouter/internal/routerkeys"
)

// TestCreatingAPoolAndInviting checks that the link is single-use and only its
// hash is stored.
func TestCreatingAPoolAndInviting(t *testing.T) {
	harness := newHarness(t)
	defer harness.close()
	alex := harness.register("alex")
	bea := harness.register("bea")
	cem := harness.register("cem")

	created := harness.api(alex, http.MethodPost, "/pools", map[string]any{
		"name": "Friends", "selfName": "alex", "sharedAccountIds": []string{alex.Account},
	})
	if created.Code != http.StatusCreated {
		t.Fatalf("create a pool: %d %s", created.Code, created.Body.String())
	}
	answer := decoded(t, created)
	poolID := answer["poolId"].(string)
	mine := poolViewIn(t, answer, poolID)
	if mine["isHost"] != true {
		t.Fatalf("the creator is not the host: %v", mine)
	}
	if len(partnersOf(t, mine)) != 0 {
		t.Fatalf("a new pool has partners: %v", mine["partners"])
	}
	if shared, _ := mine["sharedAccountIds"].([]any); len(shared) != 1 || shared[0] != alex.Account {
		t.Fatalf("sharedAccountIds = %v", mine["sharedAccountIds"])
	}

	link := harness.invite(alex, poolID)
	if !regexp.MustCompile(`/pool/join\?pool=[A-Za-z0-9_-]{32}$`).MatchString(link) {
		t.Fatalf("link = %s", link)
	}
	token := link[strings.Index(link, "pool=")+len("pool="):]
	var stored int
	if err := harness.db.R.QueryRow(`SELECT COUNT(*) FROM pool_invites WHERE token_hash = ?`, token).Scan(&stored); err != nil {
		t.Fatalf("count invites: %v", err)
	}
	if stored != 0 {
		t.Fatal("the invite token is stored in the clear")
	}

	preview := decoded(t, harness.api(bea, http.MethodPost, "/pools/preview", map[string]any{"link": link}))
	poolPreview, _ := preview["pool"].(map[string]any)
	hostPreview, _ := preview["host"].(map[string]any)
	if poolPreview["name"] != "Friends" || hostPreview["name"] != "alex" {
		t.Fatalf("preview = %v", preview)
	}

	joined := harness.join(bea, link, "bea", []string{bea.Account})
	inPool := poolViewIn(t, joined, poolID)
	if inPool["name"] != "Friends" {
		t.Fatalf("the joined pool is %v", inPool["name"])
	}
	partners := partnersOf(t, inPool)
	if len(partners) != 1 || partners[0]["name"] != "alex" {
		t.Fatalf("partners = %v", partners)
	}
	if partners[0]["state"] != "online" {
		t.Fatalf("the host is not online: %v", partners[0])
	}

	reused := harness.api(cem, http.MethodPost, "/pools/join", map[string]any{
		"link": link, "selfName": "cem", "sharedAccountIds": []string{},
	})
	reusedBody := decoded(t, reused)
	errorBody, _ := reusedBody["error"].(map[string]any)
	if errorBody["code"] != "POOL_INVITE_INVALID" {
		t.Fatalf("reusing the link answered %v", reusedBody)
	}
}

// TestThirdMemberIsIntroducedToEveryone checks that a third member is introduced to
// everyone and all three are linked.
func TestThirdMemberIsIntroducedToEveryone(t *testing.T) {
	harness := newHarness(t)
	defer harness.close()
	alex := harness.register("alex")
	bea := harness.register("bea")
	cem := harness.register("cem")
	created := decoded(t, harness.api(alex, http.MethodPost, "/pools", map[string]any{
		"name": "Friends", "selfName": "alex", "sharedAccountIds": []string{alex.Account},
	}))
	poolID := created["poolId"].(string)
	harness.join(bea, harness.invite(alex, poolID), "bea", []string{bea.Account})
	harness.join(cem, harness.invite(alex, poolID), "cem", []string{cem.Account})

	for _, who := range []*person{alex, bea, cem} {
		view := harness.firstPool(who)
		states := []string{}
		for _, partner := range partnersOf(t, view) {
			states = append(states, fmt.Sprint(partner["state"]))
		}
		if len(states) != 2 || states[0] != "online" || states[1] != "online" {
			t.Fatalf("%s sees %v", who.Name, view["partners"])
		}
	}
	// Partner keys are not on the keys page, and partners are not listed as
	// connections: they are made with the definition the pool page hides.
	var partnerKeys, partnerAccounts int
	if err := harness.db.R.QueryRow(`SELECT COUNT(*) FROM router_keys WHERE user_id = ? AND peer_link_id IS NOT NULL`,
		bea.ID).Scan(&partnerKeys); err != nil {
		t.Fatalf("count partner keys: %v", err)
	}
	if partnerKeys == 0 {
		t.Fatal("bea has no partner key at all")
	}
	if err := harness.db.R.QueryRow(`SELECT COUNT(*) FROM provider_accounts WHERE user_id = ? AND definition_id = ?`,
		bea.ID, PeerDefinition).Scan(&partnerAccounts); err != nil {
		t.Fatalf("count partner connections: %v", err)
	}
	if partnerAccounts == 0 {
		t.Fatal("bea has no partner connection at all")
	}
	shareable, _ := harness.summaryOf(bea)["shareable"].([]any)
	for _, item := range shareable {
		account, _ := item.(map[string]any)
		if account["definitionId"] == PeerDefinition {
			t.Fatalf("a partner is offered as a connection to share: %v", account)
		}
	}
}

// TestThePartnerSeesCountsOnly checks that a partner sees counts, never content or
// a per-request trail.
func TestThePartnerSeesCountsOnly(t *testing.T) {
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

	// Alex used two of Bea's shared capacity.
	beaSide, err := harness.svc.linkToMember(ctx, bea.ID, poolID, alex.MemberID)
	if err != nil || beaSide == nil {
		t.Fatalf("read bea's link: %v", err)
	}
	key := harness.partnerKey(t, bea, beaSide.ID, "alex")
	now := harness.svc.nowMS()
	for index := 0; index < 2; index++ {
		admit, err := harness.svc.Admit(ctx, key, now)
		if err != nil {
			t.Fatalf("admit: %v", err)
		}
		harness.svc.RecordFinished(ctx, key, "", true, 30)
		admit.Release(true)
	}
	// And Alex counted what he took.
	alexSide, err := harness.svc.linkToMember(ctx, alex.ID, poolID, bea.MemberID)
	if err != nil || alexSide == nil || alexSide.AccountID == nil {
		t.Fatalf("read alex's link: %v", err)
	}
	for index := 0; index < 2; index++ {
		harness.svc.RecordFinished(ctx, nil, *alexSide.AccountID, true, 30)
	}

	beaView := partnerNamed(t, harness.firstPool(bea), "alex")
	today, _ := beaView["today"].(map[string]any)
	given, _ := today["given"].(map[string]any)
	if requests, _ := given["requests"].(float64); requests < 2 {
		t.Fatalf("bea counted %v", beaView["today"])
	}
	alexView := partnerNamed(t, harness.firstPool(alex), "bea")
	alexToday, _ := alexView["today"].(map[string]any)
	taken, _ := alexToday["taken"].(map[string]any)
	if requests, _ := taken["requests"].(float64); requests < 2 {
		t.Fatalf("alex counted %v", alexView["today"])
	}
}

// partnerKey makes one partner key bound to one link.
func (h *harness) partnerKey(t *testing.T, owner *person, linkID, name string) *routerkeys.Row {
	t.Helper()
	id := "pkey-" + owner.Name + "-" + name
	if _, err := h.db.W.ExecContext(context.Background(), `INSERT OR REPLACE INTO router_keys
		(id, user_id, name, prefix, key_hash, mode, peer_link_id) VALUES (?, ?, ?, ?, ?, 'both', ?)`,
		id, owner.ID, "Pool: "+name, "nrt_"+id, "hash-"+id, linkID); err != nil {
		t.Fatalf("create partner key: %v", err)
	}
	return &routerkeys.Row{ID: id, UserID: owner.ID, PeerLinkID: &linkID}
}

// TestEachConnectionSharesItsKinds checks that each connection shares the kinds
// ticked for it, and that paid models are only shared when ticked.
func TestEachConnectionSharesItsKinds(t *testing.T) {
	harness := newHarness(t)
	defer harness.close()
	ctx := context.Background()
	bea := harness.register("bea")
	created := decoded(t, harness.api(bea, http.MethodPost, "/pools", map[string]any{
		"name": "Friends", "selfName": "bea", "sharedAccountIds": []string{bea.Account},
	}))
	poolID := created["poolId"].(string)

	// A paid connection with one paid model.
	paidAccount := harness.addAccount(bea, "custom_openai", "paidmock")
	paidRef := harness.addModel(paidAccount, "paid-bea", false)
	offered := shareableAccount(t, harness, bea, paidAccount)
	if offered["freeModels"].(float64) != 0 || offered["planModels"].(float64) != 0 ||
		offered["paidModels"].(float64) != 1 {
		t.Fatalf("a paid connection is offered as %v", offered)
	}
	share := func(body map[string]any) *testResponse {
		return harness.api(bea, http.MethodPut, "/pools/"+poolID+"/accounts/"+paidAccount+"/share", body)
	}
	kindsOf := func() []string {
		view := harness.firstPool(bea)
		all, _ := view["shareKinds"].(map[string]any)
		list, _ := all[paidAccount].([]any)
		kinds := []string{}
		for _, item := range list {
			kinds = append(kinds, fmt.Sprint(item))
		}
		return kinds
	}
	paidModel := Model{Ref: paidRef, AccountID: paidAccount, UserID: bea.ID, DefinitionID: "custom_openai"}

	// Shared with the default (free models only): its paid model stays home.
	if response := share(map[string]any{"shared": true}); response.Code != http.StatusOK {
		t.Fatalf("share: %d %s", response.Code, response.Body.String())
	}
	if fmt.Sprint(kindsOf()) != "[free]" {
		t.Fatalf("a new share offers %v", kindsOf())
	}
	if harness.svc.Offers(ctx, poolID, paidModel, harness.svc.nowMS()) {
		t.Fatal("a paid model is offered while only free is ticked")
	}
	if response := share(map[string]any{"kinds": []string{"free", "plan"}}); response.Code != http.StatusOK {
		t.Fatalf("share: %d %s", response.Code, response.Body.String())
	}
	if harness.svc.Offers(ctx, poolID, paidModel, harness.svc.nowMS()) {
		t.Fatal("a paid model is offered while only plan is ticked")
	}
	// Paid ticked: the connection serves it.
	on := share(map[string]any{"kinds": []string{"paid"}})
	if on.Code != http.StatusOK {
		t.Fatalf("share paid: %d %s", on.Code, on.Body.String())
	}
	if fmt.Sprint(kindsOf()) != "[paid]" {
		t.Fatalf("kinds = %v", kindsOf())
	}
	if !harness.svc.Offers(ctx, poolID, paidModel, harness.svc.nowMS()) {
		t.Fatal("the paid model is not offered once paid is ticked")
	}
	// Saving the list keeps each connection's kinds; leaving it out stops sharing it.
	if response := harness.api(bea, http.MethodPut, "/pools/"+poolID+"/share", map[string]any{
		"sharedAccountIds": []string{bea.Account, paidAccount},
	}); response.Code != http.StatusOK {
		t.Fatalf("save the list: %d %s", response.Code, response.Body.String())
	}
	if fmt.Sprint(kindsOf()) != "[paid]" {
		t.Fatalf("saving the list changed the kinds: %v", kindsOf())
	}
	if response := harness.api(bea, http.MethodPut, "/pools/"+poolID+"/share", map[string]any{
		"sharedAccountIds": []string{bea.Account},
	}); response.Code != http.StatusOK {
		t.Fatalf("save the list: %d %s", response.Code, response.Body.String())
	}
	if fmt.Sprint(kindsOf()) != "[]" {
		t.Fatalf("a connection left out of the list still shares %v", kindsOf())
	}
	if harness.svc.Offers(ctx, poolID, paidModel, harness.svc.nowMS()) {
		t.Fatal("a connection left out of the list is still offered")
	}
	// No kinds removes it too, and a change needs shared or kinds.
	if response := share(map[string]any{"kinds": []string{"paid"}}); response.Code != http.StatusOK {
		t.Fatalf("share: %d", response.Code)
	}
	if response := share(map[string]any{"kinds": []string{}}); response.Code != http.StatusOK {
		t.Fatalf("unshare: %d", response.Code)
	}
	if fmt.Sprint(kindsOf()) != "[]" {
		t.Fatalf("no kinds still shares %v", kindsOf())
	}
	if response := share(map[string]any{}); response.Code != http.StatusBadRequest {
		t.Fatalf("a change without shared or kinds answered %d", response.Code)
	}
	if response := share(map[string]any{"kinds": []string{"everything"}}); response.Code != http.StatusBadRequest {
		t.Fatalf("an unknown kind answered %d", response.Code)
	}
}

// shareableAccount finds one connection in the pool page's shareable list.
func shareableAccount(t *testing.T, harness *harness, who *person, accountID string) map[string]any {
	t.Helper()
	list, _ := harness.summaryOf(who)["shareable"].([]any)
	for _, item := range list {
		account, _ := item.(map[string]any)
		if account["id"] == accountID {
			return account
		}
	}
	t.Fatalf("%s does not offer %s: %v", who.Name, accountID, list)
	return nil
}

// TestTheDaysShareIsUsedUp checks that the owner's cap is enforced and that a null
// hands it back to the automatic share.
func TestTheDaysShareIsUsedUp(t *testing.T) {
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

	beaSide, err := harness.svc.linkToMember(ctx, bea.ID, poolID, alex.MemberID)
	if err != nil || beaSide == nil {
		t.Fatalf("read bea's link: %v", err)
	}
	key := harness.partnerKey(t, bea, beaSide.ID, "alex")
	now := harness.svc.nowMS()
	used := int64(0)
	admit, err := harness.svc.Admit(ctx, key, now)
	if err != nil {
		t.Fatalf("admit: %v", err)
	}
	admit.Release(true)
	used = 1

	capped := harness.api(bea, http.MethodPut, "/pools/"+poolID+"/share", map[string]any{"requestsPerDay": used + 1})
	if capped.Code != http.StatusOK {
		t.Fatalf("fix the cap: %d %s", capped.Code, capped.Body.String())
	}
	if view := poolViewIn(t, decoded(t, capped), poolID); view != nil {
		share, _ := view["share"].(map[string]any)
		if share["requestsPerDay"].(float64) != float64(used+1) {
			t.Fatalf("the cap is %v", share["requestsPerDay"])
		}
	}
	if _, err := harness.svc.Admit(ctx, key, now); err != nil {
		t.Fatalf("the last request of the day: %v", err)
	}
	_, err = harness.svc.Admit(ctx, key, now)
	var poolErr *HttpError
	if !errors.As(err, &poolErr) || poolErr.Code != "POOL_SHARE_USED_UP" {
		t.Fatalf("past the cap: %v", err)
	}
	// null hands the cap back to the automatic share.
	back := harness.api(bea, http.MethodPut, "/pools/"+poolID+"/share", map[string]any{"requestsPerDay": nil})
	if back.Code != http.StatusOK {
		t.Fatalf("clear the cap: %d %s", back.Code, back.Body.String())
	}
	view := poolViewIn(t, decoded(t, back), poolID)
	share, _ := view["share"].(map[string]any)
	if share["requestsPerDay"] != nil {
		t.Fatalf("the cap is still set: %v", share["requestsPerDay"])
	}
	if _, err := harness.svc.Admit(ctx, key, now); err != nil {
		t.Fatalf("the automatic share: %v", err)
	}
}

// TestTheQuickSwitchPauses checks that off pauses giving and using at once, and on
// restores everything.
func TestTheQuickSwitchPauses(t *testing.T) {
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

	alexSide, err := harness.svc.linkToMember(ctx, alex.ID, poolID, bea.MemberID)
	if err != nil || alexSide == nil || alexSide.AccountID == nil {
		t.Fatalf("read alex's link: %v", err)
	}
	// The key Bea calls Alex with: its pool is Alex's, which is the one pausing.
	key := harness.partnerKey(t, alex, alexSide.ID, "bea")
	now := harness.svc.nowMS()
	enabled := func(accountID string) int {
		t.Helper()
		var value int
		if err := harness.db.R.QueryRow(`SELECT enabled FROM provider_accounts WHERE id = ?`, accountID).Scan(&value); err != nil {
			t.Fatalf("read the connection: %v", err)
		}
		return value
	}

	paused := harness.api(alex, http.MethodPut, "/pools/"+poolID+"/paused", map[string]any{"paused": true})
	if paused.Code != http.StatusOK {
		t.Fatalf("pause: %d %s", paused.Code, paused.Body.String())
	}
	if view := poolViewIn(t, decoded(t, paused), poolID); view["paused"] != true {
		t.Fatalf("the pool is not paused: %v", view)
	}
	if enabled(*alexSide.AccountID) != 0 {
		t.Fatal("alex still takes from bea while paused")
	}
	_, err = harness.svc.Admit(ctx, key, now)
	var poolErr *HttpError
	if !errors.As(err, &poolErr) || poolErr.Code != "POOL_PAUSED" {
		t.Fatalf("alex still gives while paused: %v", err)
	}

	if response := harness.api(alex, http.MethodPut, "/pools/"+poolID+"/paused", map[string]any{"paused": false}); response.Code != http.StatusOK {
		t.Fatalf("unpause: %d %s", response.Code, response.Body.String())
	}
	if enabled(*alexSide.AccountID) != 1 {
		t.Fatal("alex's partner connection stayed off after unpausing")
	}
	if _, err := harness.svc.Admit(ctx, key, now); err != nil {
		t.Fatalf("after unpausing: %v", err)
	}
}

// TestLeavingRemovesBothDirections checks that leaving removes both directions at
// once, and that the host can remove and close.
func TestLeavingRemovesBothDirections(t *testing.T) {
	harness := newHarness(t)
	defer harness.close()
	alex := harness.register("alex")
	bea := harness.register("bea")
	cem := harness.register("cem")
	created := decoded(t, harness.api(alex, http.MethodPost, "/pools", map[string]any{
		"name": "Friends", "selfName": "alex", "sharedAccountIds": []string{alex.Account},
	}))
	poolID := created["poolId"].(string)
	harness.join(bea, harness.invite(alex, poolID), "bea", []string{bea.Account})
	harness.join(cem, harness.invite(alex, poolID), "cem", []string{cem.Account})

	left := harness.api(cem, http.MethodPost, "/pools/"+poolID+"/leave", nil)
	if left.Code != http.StatusOK {
		t.Fatalf("leave: %d %s", left.Code, left.Body.String())
	}
	if pools, _ := decoded(t, left)["pools"].([]any); len(pools) != 0 {
		t.Fatalf("cem is still in a pool: %v", pools)
	}
	var links int
	if err := harness.db.R.QueryRow(`SELECT COUNT(*) FROM peer_links WHERE user_id = ?`, cem.ID).Scan(&links); err != nil {
		t.Fatalf("count links: %v", err)
	}
	if links != 0 {
		t.Fatalf("cem keeps %d partner links", links)
	}
	for _, who := range []*person{alex, bea} {
		partners := partnersOf(t, harness.firstPool(who))
		if len(partners) != 1 {
			t.Fatalf("%s sees %v", who.Name, partners)
		}
	}

	removed := harness.api(alex, http.MethodDelete, "/pools/"+poolID+"/members/"+bea.MemberID, nil)
	if removed.Code != http.StatusOK {
		t.Fatalf("remove bea: %d %s", removed.Code, removed.Body.String())
	}
	if partners := partnersOf(t, poolViewIn(t, decoded(t, removed), poolID)); len(partners) != 0 {
		t.Fatalf("bea is still listed: %v", partners)
	}
	waitFor(t, "bea to hear she was removed", 5*time.Second, func() bool {
		pools, _ := harness.summaryOf(bea)["pools"].([]any)
		return len(pools) == 0
	})
	var partnerKeys, partnerAccounts int
	if err := harness.db.R.QueryRow(`SELECT COUNT(*) FROM router_keys WHERE peer_link_id IS NOT NULL`).Scan(&partnerKeys); err != nil {
		t.Fatalf("count keys: %v", err)
	}
	if partnerKeys != 0 {
		t.Fatalf("no partner key is left anywhere, found %d", partnerKeys)
	}
	if err := harness.db.R.QueryRow(`SELECT COUNT(*) FROM provider_accounts WHERE definition_id = ?`,
		PeerDefinition).Scan(&partnerAccounts); err != nil {
		t.Fatalf("count connections: %v", err)
	}
	if partnerAccounts != 0 {
		t.Fatalf("no partner connection is left, found %d", partnerAccounts)
	}

	if response := harness.api(alex, http.MethodPost, "/pools/"+poolID+"/leave", nil); response.Code != http.StatusOK {
		t.Fatalf("the host leaves: %d %s", response.Code, response.Body.String())
	}
	if view := harness.firstPool(alex); view != nil {
		t.Fatalf("alex is still in a pool: %v", view)
	}
}

// TestOnePersonInSeveralPools checks that each pool has its own sharing and switch.
func TestOnePersonInSeveralPools(t *testing.T) {
	harness := newHarness(t)
	defer harness.close()
	alex := harness.register("alex")
	bea := harness.register("bea")

	first := decoded(t, harness.api(alex, http.MethodPost, "/pools", map[string]any{
		"name": "Work", "selfName": "alex", "sharedAccountIds": []string{alex.Account},
	}))
	second := harness.api(alex, http.MethodPost, "/pools", map[string]any{
		"name": "Family", "selfName": "alex", "sharedAccountIds": []string{},
	})
	if second.Code != http.StatusCreated {
		t.Fatalf("a second pool: %d %s", second.Code, second.Body.String())
	}
	names := []string{}
	for _, item := range decoded(t, second)["pools"].([]any) {
		view, _ := item.(map[string]any)
		names = append(names, fmt.Sprint(view["name"]))
	}
	if fmt.Sprint(names) != "[Work Family]" {
		t.Fatalf("pools = %v", names)
	}
	work := first["poolId"].(string)
	family := decoded(t, second)["poolId"].(string)

	inWork := harness.api(bea, http.MethodPost, "/pools/join", map[string]any{
		"link": harness.invite(alex, work), "selfName": "bea", "sharedAccountIds": []string{bea.Account},
	})
	inFamily := harness.api(bea, http.MethodPost, "/pools/join", map[string]any{
		"link": harness.invite(alex, family), "selfName": "bea", "sharedAccountIds": []string{},
	})
	if inWork.Code != http.StatusOK || inFamily.Code != http.StatusOK {
		t.Fatalf("bea joins: %d / %d", inWork.Code, inFamily.Code)
	}
	twice := decoded(t, harness.api(bea, http.MethodPost, "/pools/preview", map[string]any{
		"link": harness.invite(alex, work),
	}))
	if twice["alreadyMember"] != true {
		t.Fatal("the preview does not say bea is already in that pool")
	}
	// Bea shares her connection in Work only: Alex reaches it through Work, not
	// Family.
	workKinds := harness.svc.shareKindsInPool(context.Background(), work, bea.Account, harness.svc.nowMS())
	familyKinds := harness.svc.shareKindsInPool(context.Background(), family, bea.Account, harness.svc.nowMS())
	if len(workKinds) == 0 || len(familyKinds) != 0 {
		t.Fatalf("sharing is not per pool: work %v, family %v", workKinds, familyKinds)
	}
	for _, view := range poolViewIn(t, harness.summaryOf(alex), work)["partners"].([]any) {
		partner, _ := view.(map[string]any)
		if partner["state"] != "online" {
			t.Fatalf("the Work partner is %v", partner["state"])
		}
	}
	// Pausing Family leaves Work running.
	if response := harness.api(alex, http.MethodPut, "/pools/"+family+"/paused", map[string]any{"paused": true}); response.Code != http.StatusOK {
		t.Fatalf("pause Family: %d", response.Code)
	}
	workView := poolViewIn(t, harness.summaryOf(alex), work)
	if workView["paused"] != false {
		t.Fatal("pausing Family paused Work too")
	}
	for _, poolID := range []string{work, family} {
		if response := harness.api(alex, http.MethodPost, "/pools/"+poolID+"/leave", nil); response.Code != http.StatusOK {
			t.Fatalf("leave %s: %d", poolID, response.Code)
		}
	}
	waitFor(t, "bea to hear both pools closed", 5*time.Second, func() bool {
		pools, _ := harness.summaryOf(bea)["pools"].([]any)
		return len(pools) == 0
	})
}

// TestSyncPicksUpAMemberWhoJoinedMeanwhile checks that a member we have no link
// with yet is introduced on the next sync, from the host's signed member list.
func TestSyncPicksUpAMemberWhoJoinedMeanwhile(t *testing.T) {
	harness := newHarness(t)
	defer harness.close()
	alex := harness.register("alex")
	bea := harness.register("bea")
	cem := harness.register("cem")
	created := decoded(t, harness.api(alex, http.MethodPost, "/pools", map[string]any{
		"name": "Friends", "selfName": "alex", "sharedAccountIds": []string{alex.Account},
	}))
	poolID := created["poolId"].(string)
	harness.join(bea, harness.invite(alex, poolID), "bea", []string{bea.Account})
	harness.join(cem, harness.invite(alex, poolID), "cem", []string{cem.Account})
	waitFor(t, "everyone to be linked", 5*time.Second, func() bool {
		return len(partnersOf(t, harness.firstPool(cem))) == 2
	})

	// Cem missed Bea: no member entry and no link, as after a member joined while he
	// was offline.
	for _, statement := range []string{
		`DELETE FROM pool_members WHERE user_id = ? AND member_id = ?`,
		`DELETE FROM peer_links WHERE user_id = ? AND member_id = ?`,
	} {
		if _, err := harness.db.W.Exec(statement, cem.ID, bea.MemberID); err != nil {
			t.Fatalf("forget bea: %v", err)
		}
	}
	if partners := partnersOf(t, harness.firstPool(cem)); len(partners) != 1 {
		t.Fatalf("cem still sees %d partners", len(partners))
	}
	synced := harness.api(cem, http.MethodPost, "/pools/"+poolID+"/sync", nil)
	if synced.Code != http.StatusOK {
		t.Fatalf("sync: %d %s", synced.Code, synced.Body.String())
	}
	waitFor(t, "cem's link to bea after the sync", 5*time.Second, func() bool {
		link, err := harness.svc.linkToMember(context.Background(), cem.ID, poolID, bea.MemberID)
		return err == nil && link != nil
	})
	member, err := harness.svc.member(context.Background(), cem.ID, poolID, bea.MemberID)
	if err != nil || member == nil {
		t.Fatalf("bea is not in cem's member list after the sync: %v", err)
	}
	if member.BoxKey == "" {
		t.Fatal("the member list carried no sealing key")
	}
	view := poolViewIn(t, decoded(t, synced), poolID)
	if view["lastSyncAt"] == nil {
		t.Fatal("the sync was not recorded")
	}
}
