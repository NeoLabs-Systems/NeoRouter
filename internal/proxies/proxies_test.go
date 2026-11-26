package proxies

// Tests of the proxy pool. Each test builds its own origin, SOCKS5 server and
// CONNECT tunnel on loopback, so the pool is exercised end to end against a
// real socket.

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"testing"
)

const (
	adminToken  = "admin-token"
	memberToken = "member-token"
)

func TestBulkImportDeduplicatesFormatsAndNeverReturnsCredentials(t *testing.T) {
	h := newHarness(t)
	h.reset()

	if resp := h.api(http.MethodGet, "", memberToken); resp.StatusCode != http.StatusForbidden {
		t.Fatalf("member list status = %d, want 403", resp.StatusCode)
	}

	// Two lines name the same proxy in different formats, two are unusable.
	text := "host.test:1080:user:secret\n" +
		"socks5://user:secret@host.test:1080\n" +
		"host2.test,1080,user,secret\n" +
		"user:secret@host3.test:1080\n" +
		"not-a-proxy\n" +
		"ftp://host.test:80"
	resp, raw := h.apiJSON(http.MethodPost, "/import", adminToken, map[string]any{"text": text})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("import status = %d body %s", resp.StatusCode, raw)
	}
	const wantImport = `{"added":3,"duplicates":1,"invalidLines":[5,6]}`
	if string(raw) != wantImport {
		t.Fatalf("import body = %s, want %s", raw, wantImport)
	}

	entry, err := Parse("socks5://[::1]:1080", "socks5")
	if err != nil || entry.Endpoint != "socks5://[::1]:1080" {
		t.Fatalf("parse ipv6 = %+v, %v", entry, err)
	}
	if _, err := Parse("socks5://user:%zz@host.test:1080", "socks5"); err == nil {
		t.Fatal("parse with a bad escape should fail")
	}

	listResp, listBody := h.apiJSON(http.MethodGet, "", adminToken, nil)
	if listResp.StatusCode != http.StatusOK {
		t.Fatalf("list status = %d", listResp.StatusCode)
	}
	if strings.Contains(string(listBody), "secret") {
		t.Fatalf("list leaked credentials: %s", listBody)
	}
	// Key order and the null latency of a fresh proxy are part of the contract.
	if !strings.Contains(string(listBody), `"failures":0,"until":0,"latencyMs":null,"active":0,"served":0`) {
		t.Fatalf("proxy row shape drifted: %s", listBody)
	}
	var listing struct {
		Proxies []Proxy    `json:"proxies"`
		Policy  PoolPolicy `json:"policy"`
	}
	if err := json.Unmarshal(listBody, &listing); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(listing.Proxies) != 3 {
		t.Fatalf("proxies = %d, want 3", len(listing.Proxies))
	}
	if !listing.Policy.Enabled || listing.Policy.FallbackDirect {
		t.Fatalf("policy = %+v, want enabled without fallback", listing.Policy)
	}
	if !strings.Contains(string(listBody), `"lastCheck":null`) {
		t.Fatalf("lastCheck should be null before the first check: %s", listBody)
	}

	rows, err := h.db.R.Query(`SELECT secret_encrypted FROM outbound_proxies`)
	if err != nil {
		t.Fatalf("query secrets: %v", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var secret string
		if err := rows.Scan(&secret); err != nil {
			t.Fatalf("scan secret: %v", err)
		}
		if strings.Contains(secret, "secret") {
			t.Fatalf("stored secret is not sealed: %q", secret)
		}
	}

	resp2, body := h.apiJSON(http.MethodPost, "/"+listing.Proxies[0].ID+"/test", adminToken,
		map[string]any{"url": "http://127.0.0.1/"})
	if resp2.StatusCode != http.StatusBadRequest {
		t.Fatalf("private test url status = %d body %s", resp2.StatusCode, body)
	}
}

func TestSOCKS5ForwardsRequestsAndProviderErrorsDoNotQuarantine(t *testing.T) {
	h := newHarness(t)
	socks := startSOCKS5(t)
	h.reset()
	h.importProxies("127.0.0.1:"+strconv.Itoa(socks.port()), "socks5")
	origin := "http://" + h.originHost

	resp, err := h.svc.Fetch(context.Background(), h.request(t, http.MethodPost, origin+"/", "test"))
	if err != nil {
		t.Fatalf("fetch through socks: %v", err)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	_ = resp.Body.Close()
	if string(body) != "through proxy" {
		t.Fatalf("body = %q", body)
	}

	limited, err := h.svc.Fetch(context.Background(), h.request(t, http.MethodGet, origin+"/limited", ""))
	if err != nil {
		t.Fatalf("fetch limited: %v", err)
	}
	if limited.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("limited status = %d", limited.StatusCode)
	}
	h.closeBody(limited)

	first := h.list()[0]
	if first.Served != 2 {
		t.Fatalf("served = %d, want 2", first.Served)
	}
	if first.Failures != 0 {
		t.Fatalf("failures = %d, want 0: an upstream status is not a proxy failure", first.Failures)
	}
	if first.LatencyMs == nil {
		t.Fatal("latency should be measured")
	}
	if socks.accepted.Load() == 0 {
		t.Fatal("the socks fixture saw no connection")
	}
}

func TestSOCKS5CredentialsAreSent(t *testing.T) {
	h := newHarness(t)
	socks := startSOCKS5Auth(t, "user", "p@ss:word")
	h.reset()
	h.importProxies("socks5://user:p%40ss%3Aword@127.0.0.1:"+strconv.Itoa(socks.port()), "socks5")

	resp, err := h.svc.Fetch(context.Background(), h.request(t, http.MethodGet, "http://"+h.originHost+"/", ""))
	if err != nil {
		t.Fatalf("fetch with socks credentials: %v", err)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	_ = resp.Body.Close()
	if string(body) != "through proxy" {
		t.Fatalf("body = %q", body)
	}
}

func TestHTTPConnectSendsProxyAuthentication(t *testing.T) {
	h := newHarness(t)
	tunnel := startConnectProxy(t, true)
	h.reset()
	h.importProxies("http://user:secret@127.0.0.1:"+strconv.Itoa(tunnel.port()), "socks5")

	resp, err := h.svc.Fetch(context.Background(), h.request(t, http.MethodGet, "http://"+h.originHost+"/", ""))
	if err != nil {
		t.Fatalf("fetch through connect proxy: %v", err)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	_ = resp.Body.Close()
	if string(body) != "through proxy" {
		t.Fatalf("body = %q", body)
	}
	if got := tunnel.lastAuth(); got != "Basic dXNlcjpzZWNyZXQ=" {
		t.Fatalf("proxy authorization = %q", got)
	}
}

func TestFailedConnectionCoolsDownFailsOverAndRecoversAfterProbe(t *testing.T) {
	h := newHarness(t)
	socks := startSOCKS5(t)
	origin := "http://" + h.originHost
	dead := closedPort(t)
	h.reset()
	h.importProxies("127.0.0.1:"+strconv.Itoa(dead), "socks5")

	_, err := h.svc.Fetch(context.Background(), h.request(t, http.MethodPost, origin+"/", "test"))
	if err == nil || !strings.Contains(err.Error(), "No healthy") {
		t.Fatalf("fetch to a dead proxy = %v, want No healthy", err)
	}
	if h.list()[0].Until <= nowMillis() {
		t.Fatal("a dead proxy should rest")
	}

	h.importProxies("127.0.0.1:"+strconv.Itoa(socks.port()), "socks5")
	resp, err := h.svc.Fetch(context.Background(), h.request(t, http.MethodGet, origin+"/", ""))
	if err != nil {
		t.Fatalf("fetch after failover: %v", err)
	}
	h.closeBody(resp)

	var good Proxy
	for _, proxy := range h.list() {
		if strings.HasSuffix(proxy.Endpoint, ":"+strconv.Itoa(socks.port())) {
			good = proxy
		}
	}
	if good.ID == "" {
		t.Fatal("socks proxy not listed")
	}
	failed, err := h.svc.Probe(context.Background(), good.ID, "http://127.0.0.1:"+strconv.Itoa(dead)+"/")
	if err != nil {
		t.Fatalf("probe dead destination: %v", err)
	}
	if failed.OK {
		t.Fatal("probing an unreachable destination should fail")
	}
	for _, proxy := range h.list() {
		if proxy.ID == good.ID && proxy.Until <= nowMillis() {
			t.Fatal("a failed probe should rest the proxy")
		}
	}
	recovered, err := h.svc.Probe(context.Background(), good.ID, origin+"/")
	if err != nil {
		t.Fatalf("probe origin: %v", err)
	}
	if !recovered.OK || recovered.Status != http.StatusOK || recovered.LatencyMs == nil {
		t.Fatalf("probe result = %+v", recovered)
	}
	for _, proxy := range h.list() {
		if proxy.ID == good.ID && proxy.Until != 0 {
			t.Fatalf("a successful probe should clear the rest, until = %d", proxy.Until)
		}
	}

	// A proxy that rejects CONNECT resends nothing: the failed tunnel is safe to
	// retry, so the direct fallback still delivers the POST exactly once.
	h.reset()
	denied := startDeniedProxy(t)
	h.importProxies("http://127.0.0.1:"+strconv.Itoa(denied.port()), "socks5")
	h.settings.setPolicy(t, true, true)
	resp, err = h.svc.Fetch(context.Background(), h.request(t, http.MethodPost, origin+"/", "test"))
	if err != nil {
		t.Fatalf("fetch with a rejecting proxy and fallback: %v", err)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	_ = resp.Body.Close()
	if string(body) != "through proxy" {
		t.Fatalf("body = %q", body)
	}
	if failures := h.list()[0].Failures; failures != 1 {
		t.Fatalf("failures = %d, want 1: a failed CONNECT is retryable", failures)
	}
}

func TestDirectFallbackAndDisabledPolicy(t *testing.T) {
	h := newHarness(t)
	origin := "http://" + h.originHost + "/"
	h.reset()

	_, err := h.svc.Fetch(context.Background(), h.request(t, http.MethodGet, origin, ""))
	if err == nil || !errors.Is(err, ErrNoHealthyProxy) {
		t.Fatalf("fetch with no proxy = %v, want ErrNoHealthyProxy", err)
	}

	h.settings.setPolicy(t, true, true)
	resp, err := h.svc.Fetch(context.Background(), h.request(t, http.MethodGet, origin, ""))
	if err != nil {
		t.Fatalf("fetch with fallback: %v", err)
	}
	h.closeBody(resp)

	h.settings.setPolicy(t, false, false)
	resp, err = h.svc.Fetch(context.Background(), h.request(t, http.MethodGet, origin, ""))
	if err != nil {
		t.Fatalf("fetch with the pool off: %v", err)
	}
	h.closeBody(resp)
}

func TestStreamsHoldTheirLeaseAndAmbiguousPOSTsAreNeverReplayed(t *testing.T) {
	h := newHarness(t)
	socks := startSOCKS5(t)
	origin := "http://" + h.originHost
	h.reset()
	h.importProxies("127.0.0.1:"+strconv.Itoa(socks.port()), "socks5")

	stream, err := h.svc.Fetch(context.Background(), h.request(t, http.MethodGet, origin+"/stream", ""))
	if err != nil {
		t.Fatalf("fetch stream: %v", err)
	}
	if active := h.list()[0].Active; active != 1 {
		t.Fatalf("active while streaming = %d, want 1", active)
	}
	chunk := make([]byte, 64)
	n, err := stream.Body.Read(chunk)
	if err != nil && !errors.Is(err, io.EOF) {
		t.Fatalf("read stream: %v", err)
	}
	if !strings.Contains(string(chunk[:n]), "data: hello") {
		t.Fatalf("stream chunk = %q", chunk[:n])
	}
	if err := stream.Body.Close(); err != nil {
		t.Fatalf("close stream: %v", err)
	}
	released := h.list()[0]
	if released.Active != 0 {
		t.Fatalf("active after cancel = %d, want 0", released.Active)
	}
	if released.Failures != 0 {
		t.Fatalf("a cancelled stream is not a failure, got %d", released.Failures)
	}

	h.settings.setPolicy(t, true, true)
	_, err = h.svc.Fetch(context.Background(), h.request(t, http.MethodPost, origin+"/drop", "test"))
	if err == nil || !strings.Contains(err.Error(), "transport failed") {
		t.Fatalf("ambiguous POST error = %v, want transport failed", err)
	}
	if dropped := h.dropped.Load(); dropped != 1 {
		t.Fatalf("origin saw %d requests, want 1: the direct fallback must not replay it", dropped)
	}
}

func TestBulkActionsDisableEnableAndDelete(t *testing.T) {
	h := newHarness(t)
	h.reset()

	_, _ = h.apiJSON(http.MethodPost, "/import", adminToken,
		map[string]any{"text": "10.9.0.1:1080\n10.9.0.2:1080\n10.9.0.3:1080", "protocol": "socks5"})
	var ids []string
	for _, proxy := range h.list() {
		if strings.Contains(proxy.Endpoint, "10.9.0.") {
			ids = append(ids, proxy.ID)
		}
	}
	if len(ids) != 3 {
		t.Fatalf("imported proxies = %d, want 3", len(ids))
	}

	_, body := h.apiJSON(http.MethodPost, "/bulk", adminToken,
		map[string]any{"action": "disable", "ids": ids[:2]})
	if string(body) != `{"changed":2}` {
		t.Fatalf("bulk disable body = %s", body)
	}
	disabled := 0
	for _, proxy := range h.list() {
		if contains(ids, proxy.ID) && !proxy.Enabled {
			disabled++
		}
	}
	if disabled != 2 {
		t.Fatalf("disabled = %d, want 2", disabled)
	}

	if resp, _ := h.apiJSON(http.MethodPost, "/bulk", memberToken, map[string]any{"action": "delete", "all": true}); resp.StatusCode != http.StatusForbidden {
		t.Fatalf("member bulk delete status = %d, want 403", resp.StatusCode)
	}
	_, body = h.apiJSON(http.MethodPost, "/bulk", adminToken, map[string]any{"action": "delete", "all": true})
	if string(body) != `{"changed":3}` {
		t.Fatalf("bulk delete body = %s", body)
	}
	if len(h.list()) != 0 {
		t.Fatalf("proxies left = %d, want 0", len(h.list()))
	}
}

func TestAutomaticCheckTestsEveryEnabledProxyAndRestsTheDead(t *testing.T) {
	h := newHarness(t)
	socks := startSOCKS5(t)
	dead := closedPort(t)
	h.reset()
	h.importProxies("127.0.0.1:"+strconv.Itoa(socks.port())+"\n"+"127.0.0.1:"+strconv.Itoa(dead), "socks5")

	result, err := h.svc.CheckAll(context.Background(), "http://"+h.originHost+"/")
	if err != nil {
		t.Fatalf("check all: %v", err)
	}
	if result.Checked != 2 || result.OK != 1 {
		t.Fatalf("check result = %+v, want 2 checked and 1 ok", result)
	}
	var resting *Proxy
	for i, proxy := range h.list() {
		if strings.HasSuffix(proxy.Endpoint, ":"+strconv.Itoa(dead)) {
			resting = &h.list()[i]
		}
	}
	if resting == nil {
		t.Fatal("dead proxy not listed")
	}
	if resting.CheckOk == nil || *resting.CheckOk {
		t.Fatalf("dead proxy checkOk = %v, want false", resting.CheckOk)
	}
	if resting.Until <= nowMillis() {
		t.Fatal("a dead proxy leaves the rotation")
	}
	last := h.svc.LastCheck()
	if last == nil || last.Checked != 2 {
		t.Fatalf("lastCheck = %+v", last)
	}
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
