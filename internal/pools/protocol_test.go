package pools

// The pool protocol's unit tests: signed envelopes, the canonical form both sides
// sign, and detached signatures (test/unit/pools.test.js).

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

// TestSignedEnvelopes checks that tampering, wrong keys, old messages and replays
// are refused.
func TestSignedEnvelopes(t *testing.T) {
	harness := newHarness(t)
	defer harness.close()
	ctx := testContext(t)
	one := harness.register("one")
	two := harness.register("two")

	message, err := harness.svc.envelope(ctx, one.ID, "leave", map[string]any{"a": 1}, &two.MemberID)
	if err != nil {
		t.Fatalf("sign a message: %v", err)
	}
	mustFail(t, "signed by another key", func() error {
		_, err := harness.svc.openEnvelope(message, "leave", two.MemberID, harness.svc.nowMS())
		return err
	}, "The signature does not match.")

	tampered := &peerEnvelope{Body: []byte(strings.Replace(string(message.Body), `"a":1`, `"a":2`, 1)),
		Signature: message.Signature}
	mustFail(t, "body changed", func() error {
		_, err := harness.svc.openEnvelope(tampered, "leave", harness.publicKeyOf(t, one.ID), harness.svc.nowMS())
		return err
	}, "The signature does not match.")

	mustFail(t, "another type", func() error {
		_, err := harness.svc.openEnvelope(message, "remove", harness.publicKeyOf(t, one.ID), harness.svc.nowMS())
		return err
	}, "Unexpected message type.")

	mustFail(t, "ten minutes later", func() error {
		_, err := harness.svc.openEnvelope(message, "leave", harness.publicKeyOf(t, one.ID),
			harness.svc.nowMS()+10*60_000)
		return err
	}, "too old")

	body, err := harness.svc.openEnvelope(message, "leave", harness.publicKeyOf(t, one.ID), harness.svc.nowMS())
	if err != nil {
		t.Fatalf("open a good message: %v", err)
	}
	if data, _ := body.data()["a"].(json.Number); data.String() != "1" {
		t.Fatalf("data.a = %v", body.data()["a"])
	}
	mustFail(t, "the same message twice", func() error {
		_, err := harness.svc.openEnvelope(message, "leave", harness.publicKeyOf(t, one.ID), harness.svc.nowMS())
		return err
	}, "already received")
}

// TestCanonicalForm checks that both sides sign the same bytes: sorted keys at
// every level, numbers without a needless fraction, and strings JSON.stringify
// would not escape differently.
func TestCanonicalForm(t *testing.T) {
	value, err := decodeJSON([]byte(`{"b":1,"a":[{"d":2,"c":3}]}`))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	text, err := canonical(value)
	if err != nil {
		t.Fatalf("canonical: %v", err)
	}
	if text != `{"a":[{"c":3,"d":2}],"b":1}` {
		t.Fatalf("canonical = %s", text)
	}
	plain, err := decodeJSON([]byte(`{"name":"Quiz <Night> & \"Co\"","n":1780000000000,"ok":true,"none":null,"f":1.5}`))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	canonicalText, err := canonical(plain)
	if err != nil {
		t.Fatalf("canonical: %v", err)
	}
	// Node's JSON.stringify leaves <, > and & alone and prints whole numbers
	// without a fraction.
	want := `{"f":1.5,"n":1780000000000,"name":"Quiz <Night> & \"Co\"","none":null,"ok":true}`
	if canonicalText != want {
		t.Fatalf("canonical = %s\nwant      %s", canonicalText, want)
	}
}

// TestDetachedSignatures covers the introduction tickets and member lists.
func TestDetachedSignatures(t *testing.T) {
	harness := newHarness(t)
	defer harness.close()
	ctx := testContext(t)
	one := harness.register("one")
	two := harness.register("two")

	ticket := map[string]any{"pool": "p", "to": two.MemberID, "exp": 1780000000000}
	signature, err := harness.svc.signObject(ctx, one.ID, ticket)
	if err != nil {
		t.Fatalf("sign a ticket: %v", err)
	}
	if !verifyObject(harness.publicKeyOf(t, one.ID), ticket, signature) {
		t.Fatal("a signed ticket does not verify")
	}
	ticket["to"] = "someone else"
	if verifyObject(harness.publicKeyOf(t, one.ID), ticket, signature) {
		t.Fatal("a changed ticket still verifies")
	}
}

// TestBadSignaturesAreRefused is the API test's check that a forged or wrongly
// signed federation message changes nothing.
func TestBadSignaturesAreRefused(t *testing.T) {
	harness := newHarness(t)
	defer harness.close()
	ctx := testContext(t)
	alex := harness.register("alex")
	bea := harness.register("bea")
	created := decoded(t, harness.api(alex, http.MethodPost, "/pools", map[string]any{
		"name": "Friends", "selfName": "alex", "sharedAccountIds": []string{alex.Account},
	}))
	poolID := created["poolId"].(string)
	link := harness.invite(alex, poolID)
	harness.join(bea, link, "bea", []string{bea.Account})
	harness.svc.FederationLimiterOf().Clear()

	// Nobody's member id: the message cannot be matched to a pool.
	forged := map[string]any{
		"body": map[string]any{"v": 2, "type": "leave", "from": "00000000-0000-0000-0000-000000000000",
			"to": "x", "ts": harness.svc.nowMS(), "nonce": "n1", "data": map[string]any{}},
		"signature": "AAAA",
	}
	if status := harness.federation(t, "leave", forged); status != http.StatusNotFound {
		t.Fatalf("a forged member id answered %d", status)
	}
	wrong := map[string]any{
		"body": map[string]any{"v": 2, "type": "leave", "from": bea.MemberID, "to": alex.MemberID,
			"ts": harness.svc.nowMS(), "nonce": "n2", "data": map[string]any{"pool": poolID}},
		"signature": strings.Repeat("A", 86) + "==",
	}
	if status := harness.federation(t, "leave", wrong); status != http.StatusUnauthorized {
		t.Fatalf("a wrongly signed message answered %d", status)
	}
	view := harness.firstPool(alex)
	if len(partnersOf(t, view)) != 1 {
		t.Fatalf("nothing changed: %v", view["partners"])
	}
	_ = ctx
}

// federation posts one federation call.
func (h *harness) federation(t *testing.T, path string, body any) int {
	t.Helper()
	encoded, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	request, err := http.NewRequest(http.MethodPost, h.base()+"/federation/v1/"+path, strings.NewReader(string(encoded)))
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := h.server.Client().Do(request)
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer response.Body.Close()
	_, _ = response.Body.Read(nil)
	return response.StatusCode
}

// publicKeyOf is one person's pool public key.
func (h *harness) publicKeyOf(t *testing.T, userID string) string {
	t.Helper()
	record, err := h.svc.identityOf(testContext(t), userID)
	if err != nil || record == nil {
		t.Fatalf("read identity: %v", err)
	}
	return record.PublicKey
}

// mustFail runs one call and checks the error's text.
func mustFail(t *testing.T, what string, call func() error, want string) {
	t.Helper()
	err := call()
	if err == nil {
		t.Fatalf("%s: no error", what)
	}
	if !strings.Contains(err.Error(), want) {
		t.Fatalf("%s: error %q does not contain %q", what, err.Error(), want)
	}
}

// testContext is a context for direct service calls.
func testContext(t *testing.T) context.Context {
	t.Helper()
	return context.Background()
}
