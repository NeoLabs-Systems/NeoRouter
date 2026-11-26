package live

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/NeoLabs-Systems/neolabs-go/passhash"
	"github.com/NeoLabs-Systems/neorouter/internal/routerkeys"
	"github.com/coder/websocket"
)

// testLogger keeps the gateway's logging out of the test output.
func testLogger(t *testing.T) *slog.Logger {
	t.Helper()
	return slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError}))
}

// waitRequests waits until the request log holds at least count rows.
func (f *fixture) waitRequests(count int) []requestRow {
	f.t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		rows := f.requests()
		if len(rows) >= count {
			return rows
		}
		if time.Now().After(deadline) {
			f.t.Fatalf("request log holds %d rows, want %d", len(rows), count)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// waitOpenError waits for the handshake result and returns its error, nil when
// the socket opened.
func (c *client) waitOpenError(t *testing.T) error {
	t.Helper()
	select {
	case err := <-c.opened:
		c.opened <- err
		return err
	case <-time.After(5 * time.Second):
		t.Fatal("open: timed out")
	}
	return nil
}

// TestSessionRelaysRewritesModelAndLogsUsage: a GPT-Live session is relayed
// with the model rewritten and usage logged.
func TestSessionRelaysRewritesModelAndLogsUsage(t *testing.T) {
	f := newFixture(t)
	c := f.open("/v1/live/sessions", bearer(f.token))
	c.waitOpened(t)

	c.send(t, `{"type":"session.start","session":{"model":"auto","instructions":"hi"}}`)
	started := c.next(t, "session.started")
	if got := started.Object("session").String("model"); got != mockLiveV1 {
		t.Fatalf("session.started model = %q, want %q (auto picked the live model)", got, mockLiveV1)
	}
	// The provider saw its own model id, not "auto".
	if seen := f.upstream.startedModels(); len(seen) != 1 || seen[0] != mockLiveV1 {
		t.Fatalf("provider saw session.start with %v, want [%s]", seen, mockLiveV1)
	}

	c.send(t, `{"type":"session.input_audio.append","audio":"AAAA"}`)
	echoed := c.next(t, "echo")
	if got := echoed.Object("event").String("audio"); got != "AAAA" {
		t.Fatalf("echoed audio = %q, want AAAA", got)
	}
	c.next(t, "response.done")

	// Gemini-style usage, sent as a binary frame.
	c.send(t, `{"type":"usage.binary"}`)
	c.next(t, "echo")
	time.Sleep(50 * time.Millisecond)

	c.close()
	if info := c.waitClosed(t); info.code != 1000 {
		t.Fatalf("close code = %d, want 1000", info.code)
	}
	row := f.waitRequests(1)[0]
	if row.apiFormat != "openai_live" {
		t.Fatalf("api_format = %q, want openai_live", row.apiFormat)
	}
	if !strings.HasSuffix(row.modelLabel, "/"+mockLiveV1) {
		t.Fatalf("model_label = %q, want the provider's own model id %s", row.modelLabel, mockLiveV1)
	}
	if row.inputTokens != 17 {
		t.Fatalf("input tokens = %d, want 17 (JSON and binary usage reports both count)", row.inputTokens)
	}
	if row.outputTokens != 8 {
		t.Fatalf("output tokens = %d, want 8", row.outputTokens)
	}
	if row.capability != "live" {
		t.Fatalf("capability = %q, want live", row.capability)
	}
	if row.status != "success" {
		t.Fatalf("status = %q, want success", row.status)
	}
	if row.httpStatus != 101 {
		t.Fatalf("http_status = %d, want 101", row.httpStatus)
	}
	if row.attempts != 1 {
		t.Fatalf("attempts = %d, want 1", row.attempts)
	}
	if row.stream != 1 {
		t.Fatalf("stream = %d, want 1", row.stream)
	}
	if row.requestedModel != "auto" {
		t.Fatalf("requested_model = %q, want auto", row.requestedModel)
	}
	if row.isFree != 1 {
		t.Fatalf("is_free = %d, want 1", row.isFree)
	}
	if !strings.Contains(row.trailJSON, `"outcome":"success"`) || !strings.Contains(row.trailJSON, mockLiveV1) {
		t.Fatalf("trail_json = %s, want the one successful attempt", row.trailJSON)
	}
	if row.errorCode != "" || row.errorMessage != "" {
		t.Fatalf("a successful session wrote an error: %q %q", row.errorCode, row.errorMessage)
	}
}

// TestRealtimeTakesModelFromURL: the realtime protocol takes the model from the
// URL.
func TestRealtimeTakesModelFromURL(t *testing.T) {
	f := newFixture(t)
	headers := bearer(f.token)
	headers.Set("OpenAI-Beta", "realtime=v1")
	c := f.open("/v1/realtime?model="+mockLiveV1, headers)
	c.waitOpened(t)

	created := c.next(t, "session.created")
	if got := created.Object("session").String("model"); got != mockLiveV1 {
		t.Fatalf("session.created model = %q, want %q", got, mockLiveV1)
	}
	// The client asked for the beta protocol, so the provider did too.
	if betas := f.upstream.betas(); len(betas) != 1 || betas[0] != "realtime=v1" {
		t.Fatalf("provider saw OpenAI-Beta %v, want [realtime=v1]", betas)
	}
	c.close()
	c.waitClosed(t)
	if row := f.waitRequests(1)[0]; row.apiFormat != "openai_realtime" {
		t.Fatalf("api_format = %q, want openai_realtime", row.apiFormat)
	}
}

// TestRefusedProviderSocketEndsSession: a refused provider socket ends the
// session with an error event.
func TestRefusedProviderSocketEndsSession(t *testing.T) {
	f := newFixture(t)
	c := f.open("/v1/realtime?model=fail-realtime", bearer(f.token))
	c.waitOpened(t)

	failure := c.next(t, "error")
	message := failure.Object("error").String("message")
	if !strings.Contains(message, "attempt") && !strings.Contains(message, "No live model") {
		t.Fatalf("error message = %q, want an attempt or no-model explanation", message)
	}
	if kind := failure.Object("error").String("type"); kind != "invalid_request_error" {
		t.Fatalf("error type = %q, want invalid_request_error", kind)
	}
	if info := c.waitClosed(t); info.code != 1011 {
		t.Fatalf("close code = %d, want 1011", info.code)
	}
	row := f.waitRequests(1)[0]
	if row.errorCode != "all_attempts_failed" {
		t.Fatalf("error_code = %q, want all_attempts_failed", row.errorCode)
	}
	if row.httpStatus != 502 {
		t.Fatalf("http_status = %d, want 502", row.httpStatus)
	}
	if row.status != "error" {
		t.Fatalf("status = %q, want error", row.status)
	}
}

// TestMissingOrWrongKeyRefused: a missing or wrong key is refused before the
// upgrade.
func TestMissingOrWrongKeyRefused(t *testing.T) {
	f := newFixture(t)
	wrong := f.open("/v1/live/sessions", bearer("nrt_wrong"))
	if err := wrong.waitOpenError(t); err == nil || !strings.Contains(err.Error(), "401") {
		t.Fatalf("open error = %v, want a 401 refusal", err)
	}
	// The key may also come as ?key= (Gemini style), with no headers at all.
	viaQuery := f.open("/v1/live/sessions?key="+f.token, nil)
	viaQuery.waitOpened(t)
	viaQuery.close()
	viaQuery.waitClosed(t)
}

// TestPoolPartnerRefused checks the second refusal of the handshake.
func TestPoolPartnerRefused(t *testing.T) {
	f := newFixture(t)
	token := routerkeys.Prefix + "_peerlinkkey1"
	if _, err := f.db.W.Exec(`INSERT INTO router_keys (id, user_id, name, prefix, key_hash, mode, peer_link_id)
		VALUES ('peer-key', ?, 'peer', 'nrt_peerlink', ?, 'free_only', 'peer-link-1')`,
		testUserID, passhash.DigestToken(token)); err != nil {
		t.Fatalf("insert peer key: %v", err)
	}
	conn, response, err := websocket.Dial(context.Background(), f.base+"/v1/live/sessions",
		&websocket.DialOptions{HTTPHeader: bearer(token)})
	if conn != nil {
		_ = conn.CloseNow()
	}
	if err == nil {
		t.Fatal("a pool partner's key opened a live session")
	}
	if response == nil || response.StatusCode != http.StatusForbidden {
		t.Fatalf("status = %v, want 403", response)
	}
	body, _ := io.ReadAll(response.Body)
	_ = response.Body.Close()
	if got := string(body); got != `{"error":{"message":"Pool partners can only use chat models."}}` {
		t.Fatalf("body = %q", got)
	}
}

// TestProviderHangUpCountsAsFailedAttempt: a provider that hangs up before
// answering counts as a failed attempt.
func TestProviderHangUpCountsAsFailedAttempt(t *testing.T) {
	f := newFixture(t)
	c := f.open("/v1/live/sessions", bearer(f.token))
	c.waitOpened(t)
	c.send(t, `{"type":"session.start","session":{"model":"fail-realtime"}}`)

	failure := c.next(t, "error")
	if message := failure.Object("error").String("message"); !strings.Contains(message, "model failed") {
		t.Fatalf("error message = %q, want the provider's own reason", message)
	}
	if info := c.waitClosed(t); info.code != 1011 {
		t.Fatalf("close code = %d, want 1011", info.code)
	}
	row := f.waitRequests(1)[0]
	if !strings.Contains(row.trailJSON, `"outcome":"server"`) {
		t.Fatalf("trail_json = %s, want the hung-up attempt recorded as a server failure", row.trailJSON)
	}
	if row.errorCode != "all_attempts_failed" {
		t.Fatalf("error_code = %q, want all_attempts_failed", row.errorCode)
	}
}

// TestCreditsNeededThroughFreeConnection: a key that may spend cannot pay
// through a connection with no billing, and the error says so.
func TestCreditsNeededThroughFreeConnection(t *testing.T) {
	f := newFixture(t)
	budgetUSD := 3.0
	paying := insertKey(t, f.db, testUserID, "both", &budgetUSD)
	// The connection has no billing, and the model is not in its free tier.
	f.catalog.setFree(testAccount, false)

	c := f.open("/v1/realtime?model="+mockLiveV1, bearer(paying))
	c.waitOpened(t)
	failure := c.next(t, "error")
	message := failure.Object("error").String("message")
	if !strings.Contains(message, "need credits, and their connection is set to free") {
		t.Fatalf("error message = %q, want the needs-credits explanation", message)
	}
	c.waitClosed(t)
}

// TestAuthFailureRevalidatesAccount checks the hook fired when a provider
// refuses its key: the connection is re-checked in the background.
func TestAuthFailureRevalidatesAccount(t *testing.T) {
	f := newFixture(t)
	// The account's key is not the one the provider accepts.
	f.accounts.add(testAccount, testUserID, f.upstream.URL(), "wrong-key", false)

	c := f.open("/v1/realtime?model="+mockLiveV1, bearer(f.token))
	c.waitOpened(t)
	failure := c.next(t, "error")
	if message := failure.Object("error").String("message"); !strings.Contains(message, "Invalid API key") {
		t.Fatalf("error message = %q, want the provider's refusal", message)
	}
	c.waitClosed(t)
	deadline := time.Now().Add(3 * time.Second)
	for len(f.accounts.validatedAccounts()) == 0 {
		if time.Now().After(deadline) {
			t.Fatal("the refused connection was never re-checked")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if got := f.accounts.validatedAccounts()[0]; got != testAccount {
		t.Fatalf("re-checked account = %q, want %q", got, testAccount)
	}
	row := f.waitRequests(1)[0]
	if !strings.Contains(row.trailJSON, `"outcome":"auth"`) {
		t.Fatalf("trail_json = %s, want the refusal recorded as an auth failure", row.trailJSON)
	}
}

// TestGeminiSessionRewritesSetup checks the Gemini dialect: the model comes from
// the setup message, goes out as models/<id>, and the key travels in the query.
func TestGeminiSessionRewritesSetup(t *testing.T) {
	f := newFixture(t)
	f.accounts.addGemini("acct-gemini", testUserID, f.upstream.gatewayURL(), mockSecret)
	f.catalog.add(geminiEntry(3, "google/gemini-live-1", "gemini-live-1", "acct-gemini"))

	c := f.open("/ws/google.ai.generativelanguage.v1beta.GenerativeService.BidiGenerateContent", bearer(f.token))
	c.waitOpened(t)
	c.send(t, `{"setup":{"model":"models/auto","generationConfig":{"responseModalities":["AUDIO"]}}}`)

	echoed := c.next(t, "echo")
	event := echoed.Object("event")
	if got := event.Object("setup").String("model"); got != "models/gemini-live-1" {
		t.Fatalf("setup.model = %q, want models/gemini-live-1", got)
	}
	// Everything else of the opening message is untouched, in the same order.
	body, err := marshalObject(event)
	if err != nil {
		t.Fatalf("encode echo: %v", err)
	}
	want := `{"setup":{"model":"models/gemini-live-1","generationConfig":{"responseModalities":["AUDIO"]}}}`
	if string(body) != want {
		t.Fatalf("opening message = %s, want %s", body, want)
	}
	c.close()
	c.waitClosed(t)
	row := f.waitRequests(1)[0]
	if row.apiFormat != "gemini_live" {
		t.Fatalf("api_format = %q, want gemini_live", row.apiFormat)
	}
	if row.modelLabel != "google/gemini-live-1" {
		t.Fatalf("model_label = %q, want google/gemini-live-1", row.modelLabel)
	}
}

// TestFirstMessageTimeout checks the protocol that names its model in the opening
// message: a client that says nothing is told so.
func TestFirstMessageTimeout(t *testing.T) {
	f := newFixture(t)
	// The gateway waits 30 s in production; this test shortens it.
	restore := firstMessageTimeout
	firstMessageTimeout = 100 * time.Millisecond
	defer func() { firstMessageTimeout = restore }()

	c := f.open("/v1/live/sessions", bearer(f.token))
	c.waitOpened(t)
	failure := c.next(t, "error")
	if message := failure.Object("error").String("message"); !strings.Contains(message, "did not start in time") {
		t.Fatalf("error message = %q", message)
	}
	if info := c.waitClosed(t); info.code != 1011 {
		t.Fatalf("close code = %d, want 1011", info.code)
	}
	row := f.waitRequests(1)[0]
	if row.errorCode != "session_timeout" {
		t.Fatalf("error_code = %q, want session_timeout", row.errorCode)
	}
}

// TestUnparseableFirstMessageIsRefused checks the opening message must name a
// model.
func TestUnparseableFirstMessageIsRefused(t *testing.T) {
	f := newFixture(t)
	c := f.open("/v1/live/sessions", bearer(f.token))
	c.waitOpened(t)
	c.send(t, `not json`)
	failure := c.next(t, "error")
	if message := failure.Object("error").String("message"); !strings.Contains(message, "must open the session") {
		t.Fatalf("error message = %q", message)
	}
	if info := c.waitClosed(t); info.code != 1011 {
		t.Fatalf("close code = %d, want 1011", info.code)
	}
}

// TestUpgradeUnclaimedPathsAreNotThisGateways checks the gateway only claims its
// three protocol paths.
func TestUpgradeUnclaimedPathsAreNotThisGateways(t *testing.T) {
	for _, path := range []string{"/v1/live/sessions", "/v1/realtime",
		"/ws/google.ai.generativelanguage.v1alpha.GenerativeService.BidiGenerateContent",
		"/ws/google.ai.generativelanguage.v1beta.GenerativeService.BidiGenerateContent"} {
		if !Claimed(path) {
			t.Fatalf("Claimed(%q) = false", path)
		}
	}
	for _, path := range []string{"/v1/chat/completions", "/ws/other", "/v1/live/sessions/extra", "/api/v1/keys"} {
		if Claimed(path) {
			t.Fatalf("Claimed(%q) = true", path)
		}
	}
}

// TestPlainRequestIs404 checks a non-upgrade request to a live path is not a
// route.
func TestPlainRequestIs404(t *testing.T) {
	f := newFixture(t)
	response, err := http.Get(f.gateway.URL + "/v1/live/sessions")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", response.StatusCode)
	}
	body, _ := io.ReadAll(response.Body)
	if !strings.Contains(string(body), `"code":"NOT_FOUND"`) {
		t.Fatalf("body = %s, want the not-found envelope", body)
	}
}

// TestBinaryFramesThatAreNotJSONAreIgnored checks that an audio frame is not
// mistaken for a usage report.
func TestBinaryFramesThatAreNotJSONAreIgnored(t *testing.T) {
	data := []byte{0x00, 0x01, 0x02, 0xff}
	if _, _, ok := countUsage(true, data); ok {
		t.Fatal("raw audio counted as a usage report")
	}
	if _, _, ok := countUsage(false, []byte("not json")); ok {
		t.Fatal("unparseable text counted as a usage report")
	}
	if _, _, ok := countUsage(true, []byte(`{"usageMetadata":{"promptTokenCount":7,"responseTokenCount":3}}`)); !ok {
		t.Fatal("Gemini's binary usage report was not read")
	}
}
