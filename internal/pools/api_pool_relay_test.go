package pools

// Pools with a member that has no public address (test/api/pool_relay.test.js).
// Alex hosts; Bea's own address does not answer, so she takes part through Alex
// (the relay); Cem has a public address. All three run on one test server, and
// every call goes over real HTTP and WebSockets, as between three machines.

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/NeoLabs-Systems/neorouter/internal/providers"
	"github.com/coder/websocket"
)

// closedAddress is an address that does not answer.
const closedAddress = "http://127.0.0.1:1"

// relayHarness is the three-person pool the relay tests share.
type relayHarness struct {
	*harness
	alex, bea, cem *person
	poolID         string
	secretPrompt   string
}

// newRelayHarness starts the pool with Alex as host.
func newRelayHarness(t *testing.T) *relayHarness {
	t.Helper()
	base := newHarness(t)
	// The relay is what providers' HTTP layer reaches relay addresses with
	// (providers.Options.Transports["relay:"] in the wired app).
	base.peers = newRelayPeers(base.svc)
	base.svc.Peers = base.peers
	alex := base.register("alex")
	bea := base.register("bea")
	cem := base.register("cem")
	for _, entry := range []*person{alex, bea, cem} {
		base.addModel(entry.Account, "ok-"+entry.Name, true)
	}
	created := decoded(t, base.api(alex, http.MethodPost, "/pools", map[string]any{
		"name": "Friends", "selfName": "alex", "sharedAccountIds": []string{alex.Account},
	}))
	return &relayHarness{harness: base, alex: alex, bea: bea, cem: cem,
		poolID: created["poolId"].(string), secretPrompt: "the-secret-recipe-42"}
}

// newRelayPeers builds the HTTP layer with the relay transport registered, the
// way the wired app registers providers.Options.Transports["relay:"].
func newRelayPeers(service *Service) *providers.Client {
	return providers.NewClient(providers.Options{
		Timeout: 5 * time.Second,
		Transports: map[string]providers.Transport{
			"relay:": func(ctx context.Context, rawURL string, req *http.Request, info providers.TransportInfo) (*http.Response, error) {
				return service.RelayTransport(ctx, rawURL, req, info)
			},
		},
	})
}

// joinThroughRelay joins Bea, whose own address does not answer.
func (r *relayHarness) joinThroughRelay(t *testing.T) {
	t.Helper()
	if _, err := r.svc.join(context.Background(), r.bea.ID, poolJoin{
		Link: r.invite(r.alex, r.poolID), SelfName: "bea", SelfURL: closedAddress,
		SharedAccountIDs: []string{r.bea.Account},
	}); err != nil {
		t.Fatalf("bea joins through the relay: %v", err)
	}
	waitFor(t, "bea's relay connection", 10*time.Second, func() bool {
		return r.svc.hub.isOnline(r.poolID, r.bea.MemberID)
	})
}

// TestMemberWithoutPublicAddressJoinsThroughTheHost checks that a NeoRouter whose
// address does not answer joins through the host, on protocol v2.
func TestMemberWithoutPublicAddressJoinsThroughTheHost(t *testing.T) {
	relay := newRelayHarness(t)
	defer relay.close()

	preview := decoded(t, relay.api(relay.bea, http.MethodPost, "/pools/preview", map[string]any{
		"link": relay.invite(relay.alex, relay.poolID),
	}))
	if preview["relay"] != true {
		t.Fatalf("the host does not offer to relay: %v", preview)
	}
	relay.joinThroughRelay(t)

	bea := relay.firstPool(relay.bea)
	relayView, _ := bea["relay"].(map[string]any)
	if relayView["viaHost"] != true {
		t.Fatalf("bea is not reached through the host: %v", bea["relay"])
	}
	if relayView["connected"] != true {
		t.Fatalf("bea is not connected to the host before introducing herself: %v", bea["relay"])
	}
	seenByHost := partnerNamed(t, relay.firstPool(relay.alex), "bea")
	if seenByHost["viaHost"] != true {
		t.Fatalf("the host does not see bea as relayed: %v", seenByHost)
	}
	// Older NeoRouters only take http(s) addresses, so they leave her out; the
	// protocol stays v2.
	card := formatRelayAddress(relay.poolID, relay.bea.MemberID)
	if !strings.HasPrefix(card, "relay://") {
		t.Fatalf("card = %s", card)
	}
	if ProtocolVersion != 2 {
		t.Fatalf("protocol version = %d", ProtocolVersion)
	}
}

// TestHostUsesTheRelayedMembersModelsAndSeesOnlySealedFrames checks that the host
// passes the traffic along without being able to read it.
func TestHostUsesTheRelayedMembersModelsAndSeesOnlySealedFrames(t *testing.T) {
	relay := newRelayHarness(t)
	defer relay.close()
	relay.joinThroughRelay(t)
	ctx := context.Background()

	conn := relay.svc.hub.connectionOf(relay.poolID, relay.bea.MemberID)
	if conn == nil {
		t.Fatal("bea has no relay connection")
	}
	var (
		mu   sync.Mutex
		seen []string
	)
	var original func(any) error
	original = conn.setDeliver(func(frame any) error {
		mu.Lock()
		seen = append(seen, encodeFrame(frame))
		mu.Unlock()
		return original(frame)
	})
	defer conn.setDeliver(original)

	endpoint, err := relay.svc.hub.localEndpoint(ctx, relay.alex.ID, relay.poolID)
	if err != nil {
		t.Fatalf("open the host's own end: %v", err)
	}
	// The plain call: the partner's models.
	models, err := endpoint.request(ctx, relay.bea.MemberID, relayRequest{
		Method: http.MethodGet, Path: "/v1/models",
		Headers: http.Header{"Authorization": []string{"Bearer " + relay.bea.PoolKey}},
	})
	if err != nil {
		t.Fatalf("ask the relayed member: %v", err)
	}
	body := readAll(t, models.Body)
	models.Body.Close()
	if models.StatusCode != http.StatusOK {
		t.Fatalf("models through the relay: %d %s", models.StatusCode, body)
	}
	if !strings.Contains(body, "ok-bea") {
		t.Fatalf("the partner's models did not come back: %s", body)
	}
	// The streamed call: the prompt never travels in the clear.
	before := relay.catalog.callCount()
	request := fmt.Sprintf(`{"model":"ok-bea","messages":[{"role":"user","content":%q}]}`, relay.secretPrompt)
	chat, err := endpoint.request(ctx, relay.bea.MemberID, relayRequest{
		Method: http.MethodPost, Path: "/v1/chat/completions",
		Headers: http.Header{"Authorization": []string{"Bearer " + relay.bea.PoolKey},
			"Content-Type": []string{"application/json"}},
		Body: strings.NewReader(request),
	})
	if err != nil {
		t.Fatalf("chat through the relay: %v", err)
	}
	answer := readAll(t, chat.Body)
	chat.Body.Close()
	if chat.StatusCode != http.StatusOK || !strings.Contains(answer, "hello") {
		t.Fatalf("chat through the relay: %d %s", chat.StatusCode, answer)
	}
	if relay.catalog.callCount() != before+1 {
		t.Fatal("the partner did not serve the request")
	}
	mu.Lock()
	wire := strings.Join(seen, "\n")
	mu.Unlock()
	if !strings.Contains(wire, `"t":"open"`) {
		t.Fatalf("frames went through the hub: %s", wire)
	}
	if strings.Contains(wire, relay.secretPrompt) {
		t.Fatal("the prompt travelled in the clear")
	}
	if strings.Contains(wire, "nrt_") {
		t.Fatal("the partner key travelled in the clear")
	}
}

// TestReplayedRequestIsRefused checks that a request the host sends twice is
// refused, and cannot make the member reuse a key.
func TestReplayedRequestIsRefused(t *testing.T) {
	relay := newRelayHarness(t)
	defer relay.close()
	relay.joinThroughRelay(t)
	ctx := context.Background()

	conn := relay.svc.hub.connectionOf(relay.poolID, relay.bea.MemberID)
	original := conn.deliver
	var (
		kept   relayFrame
		keptMu sync.Mutex
	)
	conn.deliver = func(frame any) error {
		parsed, ok := parseRelayFrame(encodeFrame(frame))
		if ok && parsed.typ() == "open" {
			keptMu.Lock()
			kept = parsed
			keptMu.Unlock()
		}
		return original(frame)
	}
	firstEndpoint, err := relay.svc.hub.localEndpoint(ctx, relay.alex.ID, relay.poolID)
	if err != nil {
		t.Fatalf("open the host's own end: %v", err)
	}
	_ = firstEndpoint
	local := relay.svc.hub.localEndOf(relay.alex.ID, relay.poolID)
	if local == nil {
		t.Fatal("the host has no end of its own")
	}
	var (
		heads   []relayFrame
		headsMu sync.Mutex
	)
	var originalLocal func(any) error
	originalLocal = local.conn.setDeliver(func(frame any) error {
		parsed, ok := parseRelayFrame(encodeFrame(frame))
		if ok && (parsed.typ() == "head" || parsed.typ() == "fail") {
			headsMu.Lock()
			heads = append(heads, parsed)
			headsMu.Unlock()
		}
		return originalLocal(frame)
	})
	defer func() {
		conn.setDeliver(original)
		local.conn.setDeliver(originalLocal)
	}()

	endpoint, err := relay.svc.hub.localEndpoint(ctx, relay.alex.ID, relay.poolID)
	if err != nil {
		t.Fatalf("open the host's own end: %v", err)
	}
	response, err := endpoint.request(ctx, relay.bea.MemberID, relayRequest{
		Method: http.MethodGet, Path: "/v1/models",
		Headers: http.Header{"Authorization": []string{"Bearer " + relay.bea.PoolKey}},
	})
	if err != nil {
		t.Fatalf("ask the relayed member: %v", err)
	}
	readAll(t, response.Body)
	response.Body.Close()
	headsMu.Lock()
	firstSalt := ""
	for _, frame := range heads {
		if frame.typ() == "head" {
			if salt, ok := frame.text("r"); ok {
				firstSalt = salt
			}
		}
	}
	heads = nil
	headsMu.Unlock()
	if firstSalt == "" {
		t.Fatal("the answer carries no fresh salt")
	}

	before := relay.catalog.callCount()
	keptMu.Lock()
	replay := kept
	keptMu.Unlock()
	if replay == nil {
		t.Fatal("no sealed request was seen")
	}
	// The host sends the same sealed request again, addressed to Bea as the first
	// one was.
	sentAS, _ := replay.text("s")
	sentSeq, _ := replay.integer("seq")
	sentData, _ := replay.text("d")
	relay.svc.hub.route(local.conn, relayFrame{
		"t": "open", "s": sentAS, "to": relay.bea.MemberID, "seq": float64(sentSeq), "d": sentData,
	})
	waitFor(t, "the partner to refuse the replay", 5*time.Second, func() bool {
		headsMu.Lock()
		defer headsMu.Unlock()
		return len(heads) > 0
	})
	headsMu.Lock()
	answer := heads[0]
	headsMu.Unlock()
	if answer.typ() != "fail" {
		t.Fatalf("the replay answered %v", answer.typ())
	}
	if code, _ := answer.text("code"); code != "replayed" {
		t.Fatalf("the replay answered %q", code)
	}
	if relay.catalog.callCount() != before {
		t.Fatal("the replayed request ran at the partner")
	}
}

// TestMemberListCannotSwapSealingKey checks that a later list cannot swap the
// sealing key of a member we know.
func TestMemberListCannotSwapSealingKey(t *testing.T) {
	relay := newRelayHarness(t)
	defer relay.close()
	relay.joinThroughRelay(t)
	ctx := context.Background()

	known, err := relay.svc.member(ctx, relay.bea.ID, relay.poolID, relay.alex.MemberID)
	if err != nil || known == nil {
		t.Fatalf("read the host's member entry: %v", err)
	}
	fakeBox, fakeSig, fakeKey := fakeSealingCard(t)
	swapped := *known
	swapped.PublicKey = fakeKey
	swapped.BoxKey = fakeBox
	swapped.BoxSig = fakeSig
	if err := relay.svc.rememberMember(ctx, relay.bea.ID, relay.poolID, &swapped); err != nil {
		t.Fatalf("remember the swapped member: %v", err)
	}
	after, err := relay.svc.member(ctx, relay.bea.ID, relay.poolID, relay.alex.MemberID)
	if err != nil || after == nil {
		t.Fatalf("read the host's member entry: %v", err)
	}
	if after.PublicKey != known.PublicKey {
		t.Fatal("a later list swapped the pool key")
	}
	if after.BoxKey != known.BoxKey {
		t.Fatal("a later list swapped the sealing key")
	}
}

// fakeSealingCard makes a self-signed sealing key that is not anybody's pool key.
func fakeSealingCard(t *testing.T) (boxKey, boxSig, publicKey string) {
	t.Helper()
	sealing := newTestMember(t, "fake")
	record := newDetachedKey(t)
	signature := signWith(t, record, boxKeyLabel+sealing.boxKey)
	return sealing.boxKey, signature, record.publicKey
}

// TestPublicMemberLinksWithTheRelayedOneThroughTheHost checks that a member with a
// public address links with the relayed one through the host.
func TestPublicMemberLinksWithTheRelayedOneThroughTheHost(t *testing.T) {
	relay := newRelayHarness(t)
	defer relay.close()
	relay.joinThroughRelay(t)

	response := relay.api(relay.cem, http.MethodPost, "/pools/join", map[string]any{
		"link": relay.invite(relay.alex, relay.poolID), "selfName": "cem",
		"sharedAccountIds": []string{relay.cem.Account},
	})
	if response.Code != http.StatusOK {
		t.Fatalf("cem joins: %d %s", response.Code, response.Body.String())
	}
	cem := relay.firstPool(relay.cem)
	relayView, _ := cem["relay"].(map[string]any)
	if relayView["viaHost"] != false {
		t.Fatalf("cem is relayed although he answers directly: %v", cem["relay"])
	}
	waitFor(t, "cem's link to bea", 10*time.Second, func() bool {
		link, err := relay.svc.linkToMember(context.Background(), relay.cem.ID, relay.poolID, relay.bea.MemberID)
		return err == nil && link != nil
	})
	ctx := context.Background()
	endpoint := relay.svc.endpointFor(ctx, relay.cem.ID, relay.poolID)
	if endpoint == nil {
		t.Fatal("cem has no end of the relay")
	}
	through := relay.svc.mustRequest(t, endpoint, relay.bea.MemberID, relay.bea.PoolKey)
	if !strings.Contains(through, "ok-bea") {
		t.Fatalf("cem did not reach bea's model through the host: %s", through)
	}
	// Bea reaches Cem directly.
	waitFor(t, "bea's link to cem", 10*time.Second, func() bool {
		link, err := relay.svc.linkToMember(context.Background(), relay.bea.ID, relay.poolID, relay.cem.MemberID)
		return err == nil && link != nil
	})
	beaEndpoint := relay.svc.endpointFor(ctx, relay.bea.ID, relay.poolID)
	if beaEndpoint == nil {
		t.Fatal("bea has no end of the relay")
	}
	direct := relay.svc.mustRequest(t, beaEndpoint, relay.cem.MemberID, relay.cem.PoolKey)
	if !strings.Contains(direct, "ok-cem") {
		t.Fatalf("bea did not reach cem: %s", direct)
	}
}

// mustRequest asks one partner for its models through one end of the relay.
func (s *Service) mustRequest(t *testing.T, endpoint *relayEndpoint, memberID, key string) string {
	t.Helper()
	response, err := endpoint.request(context.Background(), memberID, relayRequest{
		Method: http.MethodGet, Path: "/v1/models",
		Headers: http.Header{"Authorization": []string{"Bearer " + key}},
	})
	if err != nil {
		t.Fatalf("ask %s: %v", memberID, err)
	}
	defer response.Body.Close()
	body := readAll(t, response.Body)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("ask %s: %d %s", memberID, response.StatusCode, body)
	}
	return body
}

// TestTheRelayedMemberAnswersOnlyPartnerCalls checks that the app is only reached
// for what a partner may call.
func TestTheRelayedMemberAnswersOnlyPartnerCalls(t *testing.T) {
	relay := newRelayHarness(t)
	defer relay.close()
	relay.joinThroughRelay(t)
	ctx := context.Background()

	endpoint, err := relay.svc.hub.localEndpoint(ctx, relay.alex.ID, relay.poolID)
	if err != nil {
		t.Fatalf("open the host's own end: %v", err)
	}
	refused, err := endpoint.request(ctx, relay.bea.MemberID, relayRequest{
		Method: http.MethodGet, Path: "/api/v1/keys",
		Headers: http.Header{"Authorization": []string{"Bearer " + relay.bea.PoolKey}},
	})
	if err != nil {
		t.Fatalf("ask for the keys page: %v", err)
	}
	defer refused.Body.Close()
	body := readAll(t, refused.Body)
	if refused.StatusCode != http.StatusForbidden {
		t.Fatalf("the keys page answered %d %s", refused.StatusCode, body)
	}
	if !strings.Contains(body, "POOL_RELAY_REFUSED") {
		t.Fatalf("the refusal is not the pool one: %s", body)
	}
	wrong, err := endpoint.request(ctx, relay.bea.MemberID, relayRequest{
		Method: http.MethodGet, Path: "/v1/models",
		Headers: http.Header{"Authorization": []string{"Bearer nrt_00000000_notarealkeynotarealkey"}},
	})
	if err != nil {
		t.Fatalf("ask with a wrong key: %v", err)
	}
	defer wrong.Body.Close()
	readAll(t, wrong.Body)
	if wrong.StatusCode != http.StatusUnauthorized {
		t.Fatalf("a wrong key answered %d, the usual key check still applies", wrong.StatusCode)
	}
}

// TestRelayLoginNeedsAValidSignature checks that logging in needs a valid
// signature by a current member.
func TestRelayLoginNeedsAValidSignature(t *testing.T) {
	relay := newRelayHarness(t)
	defer relay.close()
	relay.joinThroughRelay(t)
	// Cem is a member with a public address; his signature is what logs in.
	if response := relay.api(relay.cem, http.MethodPost, "/pools/join", map[string]any{
		"link": relay.invite(relay.alex, relay.poolID), "selfName": "cem",
		"sharedAccountIds": []string{relay.cem.Account},
	}); response.Code != http.StatusOK {
		t.Fatalf("cem joins: %d %s", response.Code, response.Body.String())
	}

	host := relay.alex.MemberID
	other := relay.bea
	valid := func(nonce string) string {
		signature, err := relay.svc.sign(context.Background(), relay.cem.ID,
			helloText(nonce, relay.poolID, relay.cem.MemberID, host))
		if err != nil {
			t.Fatalf("sign: %v", err)
		}
		return signature
	}
	forged := func(string) string { return base64.StdEncoding.EncodeToString(make([]byte, 64)) }
	otherKey := func(nonce string) string {
		signature, err := relay.svc.sign(context.Background(), other.ID,
			helloText(nonce, relay.poolID, relay.cem.MemberID, host))
		if err != nil {
			t.Fatalf("sign: %v", err)
		}
		return signature
	}
	if result := relay.attemptLogin(t, forged); result != relayCloseNotMember {
		t.Fatalf("a forged signature answered %v, want %d", result, relayCloseNotMember)
	}
	if result := relay.attemptLogin(t, otherKey); result != relayCloseNotMember {
		t.Fatalf("someone else's key answered %v, want %d", result, relayCloseNotMember)
	}
	if result := relay.attemptLogin(t, valid); result != "ready" {
		t.Fatalf("a valid signature answered %v", result)
	}
}

// attemptLogin tries to log in to the relay and reports the answer.
func (r *relayHarness) attemptLogin(t *testing.T, sign func(nonce string) string) any {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	ws, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(r.base(), "http")+RelayPath, nil)
	if err != nil {
		t.Fatalf("dial the relay: %v", err)
	}
	defer ws.CloseNow()
	for {
		_, data, err := ws.Read(ctx)
		if err != nil {
			return closeCode(err)
		}
		frame, ok := parseRelayFrame(string(data))
		if !ok {
			continue
		}
		switch frame.typ() {
		case "challenge":
			nonce, _ := frame.text("nonce")
			hello := helloFrame{T: "hello", Pool: r.poolID, Member: r.cem.MemberID, Sig: sign(nonce)}
			if err := ws.Write(ctx, websocket.MessageText, []byte(encodeFrame(hello))); err != nil {
				t.Fatalf("send hello: %v", err)
			}
		case "ready":
			_ = ws.Close(websocket.StatusNormalClosure, "")
			return "ready"
		}
	}
}

// closeCode is the relay's close code of one read error.
func closeCode(err error) int {
	return int(websocket.CloseStatus(err))
}

// TestDroppedConnectionComesBack checks that a connection the host lost comes back
// by itself.
func TestDroppedConnectionComesBack(t *testing.T) {
	relay := newRelayHarness(t)
	defer relay.close()
	relay.joinThroughRelay(t)
	ctx := context.Background()

	dropped := relay.svc.hub.connectionOf(relay.poolID, relay.bea.MemberID)
	if dropped == nil {
		t.Fatal("bea has no relay connection")
	}
	dropped.ws.terminate()
	waitFor(t, "bea to reconnect", 15*time.Second, func() bool {
		current := relay.svc.hub.connectionOf(relay.poolID, relay.bea.MemberID)
		return current != nil && current != dropped && relay.svc.hub.isOnline(relay.poolID, relay.bea.MemberID)
	})
	endpoint, err := relay.svc.hub.localEndpoint(ctx, relay.alex.ID, relay.poolID)
	if err != nil {
		t.Fatalf("open the host's own end: %v", err)
	}
	waitFor(t, "a chat through the relay after reconnecting", 10*time.Second, func() bool {
		response, err := endpoint.request(ctx, relay.bea.MemberID, relayRequest{
			Method: http.MethodGet, Path: "/v1/models",
			Headers: http.Header{"Authorization": []string{"Bearer " + relay.bea.PoolKey}},
		})
		if err != nil {
			return false
		}
		defer response.Body.Close()
		body := readAll(t, response.Body)
		return response.StatusCode == http.StatusOK && strings.Contains(body, "ok-bea")
	})
}

// TestRemovingARelayedMemberReachesHer checks that removing a relayed member
// reaches her through the host.
func TestRemovingARelayedMemberReachesHer(t *testing.T) {
	relay := newRelayHarness(t)
	defer relay.close()
	relay.joinThroughRelay(t)

	response := relay.api(relay.alex, http.MethodDelete, "/pools/"+relay.poolID+"/members/"+relay.bea.MemberID, nil)
	if response.Code != http.StatusOK {
		t.Fatalf("remove bea: %d %s", response.Code, response.Body.String())
	}
	waitFor(t, "bea to hear she was removed", 10*time.Second, func() bool {
		pools, _ := relay.summaryOf(relay.bea)["pools"].([]any)
		return len(pools) == 0
	})
	waitFor(t, "her relay connection to end", 10*time.Second, func() bool {
		return !relay.svc.hub.isOnline(relay.poolID, relay.bea.MemberID)
	})
}

// TestHostSwitchOff checks that with the host's switch off nobody is relayed and
// new joiners need their own address.
func TestHostSwitchOff(t *testing.T) {
	relay := newRelayHarness(t)
	defer relay.close()
	relay.joinThroughRelay(t)

	off := relay.api(relay.alex, http.MethodPut, "/pools/"+relay.poolID+"/relay", map[string]any{"enabled": false})
	if off.Code != http.StatusOK {
		t.Fatalf("switch the relay off: %d %s", off.Code, off.Body.String())
	}
	view := poolViewIn(t, decoded(t, off), relay.poolID)
	relayView, _ := view["relay"].(map[string]any)
	if relayView["offered"] != false {
		t.Fatalf("the host still offers to relay: %v", view["relay"])
	}
	waitFor(t, "bea's relay connection to end", 10*time.Second, func() bool {
		return !relay.svc.hub.isOnline(relay.poolID, relay.bea.MemberID)
	})
	preview := decoded(t, relay.api(relay.cem, http.MethodPost, "/pools/preview", map[string]any{
		"link": relay.invite(relay.alex, relay.poolID),
	}))
	if preview["relay"] != false {
		t.Fatalf("the preview still offers the relay: %v", preview)
	}
	_, err := relay.svc.join(context.Background(), relay.cem.ID, poolJoin{
		Link: relay.invite(relay.alex, relay.poolID), SelfName: "cem", SelfURL: "not a url",
		SharedAccountIDs: []string{},
	})
	var poolErr *HttpError
	if err == nil || !asPoolError(err, &poolErr) || poolErr.Code != "POOL_URL_UNKNOWN" {
		t.Fatalf("a joiner without an address: %v", err)
	}
	if on := relay.api(relay.alex, http.MethodPut, "/pools/"+relay.poolID+"/relay", map[string]any{"enabled": true}); on.Code != http.StatusOK {
		t.Fatalf("switch the relay back on: %d %s", on.Code, on.Body.String())
	}
}

// readAll reads one small body.
func readAll(t *testing.T, reader io.Reader) string {
	t.Helper()
	if reader == nil {
		return ""
	}
	raw, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	return string(raw)
}

// asPoolError is errors.As, kept local so the tests read plainly.
func asPoolError(err error, target **HttpError) bool {
	return errors.As(err, target)
}

// callCount is how many chat completions the partner served.
func (f *fakeCatalog) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

// newDetachedKey is a bare Ed25519 pool key for the swap test.
type detachedKey struct {
	publicKey  string
	privateKey ed25519.PrivateKey
}

// newDetachedKey makes one self-made pool key.
func newDetachedKey(t *testing.T) detachedKey {
	t.Helper()
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	encoded, err := x509.MarshalPKIXPublicKey(public)
	if err != nil {
		t.Fatalf("encode key: %v", err)
	}
	return detachedKey{publicKey: base64.StdEncoding.EncodeToString(encoded), privateKey: private}
}

// signWith signs one text with a bare pool key.
func signWith(t *testing.T, key detachedKey, text string) string {
	t.Helper()
	return base64.StdEncoding.EncodeToString(ed25519.Sign(key.privateKey, []byte(text)))
}
