package pools

// The relay's unit tests: pair keys, sealed frames, relay addresses, the calls
// a partner may make, and stream frame checking.

import (
	"crypto/ecdh"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"strings"
	"testing"
)

const (
	testPool = "11111111-1111-4111-8111-111111111111"
	testA    = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	testB    = "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"
	testC    = "cccccccc-cccc-4ccc-8ccc-cccccccccccc"
)

// testMember is one member's sealing keys.
type testMember struct {
	memberID string
	boxKey   string
	own      sealingKey
}

// newTestMember makes one member's X25519 pair.
func newTestMember(t *testing.T, memberID string) testMember {
	t.Helper()
	private, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	publicDER, err := x509.MarshalPKIXPublicKey(private.PublicKey())
	if err != nil {
		t.Fatalf("encode key: %v", err)
	}
	boxKey := base64.StdEncoding.EncodeToString(publicDER)
	return testMember{memberID: memberID, boxKey: boxKey,
		own: sealingKey{PrivateKey: private, OwnBoxKey: boxKey, OwnMemberID: memberID}}
}

// pair of one member toward another.
func (m testMember) partner(other testMember) sealPartner {
	return sealPartner{MemberID: other.memberID, BoxKey: other.boxKey}
}

// TestPairKeys checks that both members derive the same pair key, and that another
// pool or another member's sealing key does not.
func TestPairKeys(t *testing.T) {
	service := New(nil)
	alice := newTestMember(t, testA)
	bob := newTestMember(t, testB)
	first, err := service.pairKey(alice.own, alice.partner(bob), testPool)
	if err != nil {
		t.Fatalf("pair key: %v", err)
	}
	second, err := service.pairKey(bob.own, bob.partner(alice), testPool)
	if err != nil {
		t.Fatalf("pair key: %v", err)
	}
	if string(first) != string(second) {
		t.Fatal("the two sides derive different pair keys")
	}
	otherPool, err := service.pairKey(alice.own, alice.partner(bob), "22222222-2222-4222-8222-222222222222")
	if err != nil {
		t.Fatalf("pair key: %v", err)
	}
	if string(otherPool) == string(first) {
		t.Fatal("another pool derives the same pair key")
	}
	stranger := newTestMember(t, testB)
	otherKey, err := service.pairKey(alice.own, alice.partner(stranger), testPool)
	if err != nil {
		t.Fatalf("pair key: %v", err)
	}
	if string(otherKey) == string(first) {
		t.Fatal("a different sealing key derives the same pair key")
	}
}

// TestSealedFrames covers a sealed frame opening on the other side, in order, and
// every way one does not.
func TestSealedFrames(t *testing.T) {
	service := New(nil)
	alice := newTestMember(t, testA)
	bob := newTestMember(t, testB)
	pairAB, err := service.pairKey(alice.own, alice.partner(bob), testPool)
	if err != nil {
		t.Fatalf("pair key: %v", err)
	}
	pairBA, err := service.pairKey(bob.own, bob.partner(alice), testPool)
	if err != nil {
		t.Fatalf("pair key: %v", err)
	}
	stream := func(pair []byte, from, to, streamID string) *direction {
		direction, err := directionFor(pair, testPool, streamID, from, to, "")
		if err != nil {
			t.Fatalf("direction: %v", err)
		}
		return direction
	}

	out := stream(pairAB, testA, testB, "stream-1")
	back := stream(pairBA, testA, testB, "stream-1")
	firstSeq, firstData := out.sealFrame("open", []byte("hello"))
	secondSeq, secondData := out.sealFrame("data", []byte("world"))
	if plain, err := back.openFrame("open", firstSeq, firstData); err != nil || string(plain) != "hello" {
		t.Fatalf("first frame: %q %v", plain, err)
	}
	if plain, err := back.openFrame("data", secondSeq, secondData); err != nil || string(plain) != "world" {
		t.Fatalf("second frame: %q %v", plain, err)
	}

	// Changed bytes do not open.
	tampered := []byte(firstData)
	tampered[0] ^= 'A'
	if _, err := stream(pairBA, testA, testB, "stream-1").openFrame("open", 0, string(tampered)); err == nil {
		t.Fatal("a changed frame opened")
	}
	// A replay is refused.
	receiver := stream(pairBA, testA, testB, "stream-1")
	if _, err := receiver.openFrame("open", firstSeq, firstData); err != nil {
		t.Fatalf("first frame: %v", err)
	}
	if _, err := receiver.openFrame("open", firstSeq, firstData); err == nil ||
		!strings.Contains(err.Error(), "order") {
		t.Fatalf("a replay: %v", err)
	}
	// A skipped frame is refused.
	editor := stream(pairAB, testA, testB, "stream-1")
	_, _ = editor.sealFrame("data", []byte("one"))
	skipSeq, skipData := editor.sealFrame("data", []byte("two"))
	if _, err := stream(pairBA, testA, testB, "stream-1").openFrame("data", skipSeq, skipData); err == nil ||
		!strings.Contains(err.Error(), "order") {
		t.Fatalf("a skipped frame: %v", err)
	}
	// Another stream, the other direction, another frame type and a third member all
	// fail.
	if _, err := stream(pairBA, testA, testB, "stream-2").openFrame("open", 0, firstData); err == nil {
		t.Fatal("another stream opened it")
	}
	if _, err := stream(pairBA, testB, testA, "stream-1").openFrame("open", 0, firstData); err == nil {
		t.Fatal("the other direction opened it")
	}
	if _, err := stream(pairBA, testA, testB, "stream-1").openFrame("data", 0, firstData); err == nil {
		t.Fatal("another frame type opened it")
	}
	stranger := newTestMember(t, testC)
	strangerPair, err := service.pairKey(stranger.own, stranger.partner(alice), testPool)
	if err != nil {
		t.Fatalf("pair key: %v", err)
	}
	if _, err := stream(strangerPair, testA, testB, "stream-1").openFrame("open", 0, firstData); err == nil {
		t.Fatal("a third member opened it")
	}
}

// TestRelayAddresses covers the addresses that name the pool and the member.
func TestRelayAddresses(t *testing.T) {
	value := formatRelayAddress(testPool, testB)
	if value != "relay://"+testPool+"."+testB {
		t.Fatalf("address = %s", value)
	}
	parsed, ok := parseRelayAddress(value + "/v1/models?x=1")
	if !ok || parsed.PoolID != testPool || parsed.MemberID != testB || parsed.Path != "/v1/models?x=1" {
		t.Fatalf("parsed = %+v (%v)", parsed, ok)
	}
	if _, ok := parseRelayAddress("https://router.example.com"); ok {
		t.Fatal("an https address parsed as a relay address")
	}
	if _, ok := parseRelayAddress("relay://evil.example.com"); ok {
		t.Fatal("an unknown relay address parsed")
	}
	// Older NeoRouters accept only http(s) addresses, so they leave such members out.
	if strings.HasPrefix(value, "http") {
		t.Fatal("a relay address looks like an http one")
	}
	if originOf(value) != value {
		t.Fatalf("originOf(%q) = %q", value, originOf(value))
	}
}

// TestPartnerCallsAllowed checks that a relayed member answers only partner calls.
func TestPartnerCallsAllowed(t *testing.T) {
	for _, call := range [][2]string{
		{"GET", "/v1/models"},
		{"GET", "/v1/models?all=1"},
		{"POST", "/v1/chat/completions"},
		{"POST", "/federation/v1/introduce"},
	} {
		if !allowed(call[0], call[1]) {
			t.Fatalf("%s %s is refused", call[0], call[1])
		}
	}
	for _, call := range [][2]string{
		{"GET", "/api/v1/keys"},
		{"POST", "/federation/v1/join"},
		{"POST", "/v1/chat/completions/../../api/v1/admin"},
		{"DELETE", "/v1/models"},
	} {
		if allowed(call[0], call[1]) {
			t.Fatalf("%s %s is allowed", call[0], call[1])
		}
	}
}

// TestStreamFramesChecked checks that stream frames are checked before they are
// passed along.
func TestStreamFramesChecked(t *testing.T) {
	streamID := strings.Repeat("A", 22)
	valid := []relayFrame{
		{"t": "open", "s": streamID, "to": testB, "seq": float64(0), "d": "x"},
		{"t": "cancel", "s": streamID},
		{"t": "fail", "s": streamID, "code": "busy"},
	}
	for _, frame := range valid {
		if err := validStreamFrame(frame); err != nil {
			t.Fatalf("%v: %v", frame, err)
		}
	}
	invalid := []relayFrame{
		{"t": "open", "s": "short", "to": testB, "seq": float64(0), "d": "x"},
		{"t": "open", "s": streamID, "to": "not-a-member", "seq": float64(0), "d": "x"},
		{"t": "data", "s": streamID, "seq": float64(-1), "d": "x"},
		{"t": "hello", "s": streamID},
		{"t": "head", "s": streamID, "seq": float64(0), "d": "x"},
		{"t": "open", "s": streamID, "to": testB, "seq": 0.5, "d": "x"},
		{"t": "fail", "s": streamID, "code": "NOT A CODE"},
	}
	for _, frame := range invalid {
		if err := validStreamFrame(frame); err == nil {
			t.Fatalf("%v was accepted", frame)
		}
	}
}

// TestHelloText keeps the text a member signs in one place.
func TestHelloText(t *testing.T) {
	text := helloText("nonce", testPool, testA, testB)
	if text != "neorouter-pool-relay-hello|nonce|"+testPool+"|"+testA+"|"+testB {
		t.Fatalf("hello text = %s", text)
	}
}
