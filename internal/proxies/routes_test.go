package proxies

// The admin API contract: validation error texts, the 404 for an unknown proxy,
// the 204s, and the test-URL guard.

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"testing"
)

func TestPolicyValidationKeepsZodTexts(t *testing.T) {
	h := newHarness(t)
	h.reset()

	cases := []struct {
		name    string
		body    string
		status  int
		code    string
		message string
		details string
	}{
		{
			name: "missing fields", body: `{}`, status: http.StatusBadRequest, code: "VALIDATION_ERROR",
			message: "enabled: Required",
			details: `{"formErrors":[],"fieldErrors":{"enabled":["Required"],"fallbackDirect":["Required"]}}`,
		},
		{
			name: "wrong type", body: `{"enabled":"yes","fallbackDirect":true}`,
			status: http.StatusBadRequest, code: "VALIDATION_ERROR",
			message: "enabled: Expected boolean, received string",
			details: `{"formErrors":[],"fieldErrors":{"enabled":["Expected boolean, received string"]}}`,
		},
		{
			name: "not an object", body: `[1,2]`, status: http.StatusBadRequest, code: "VALIDATION_ERROR",
			message: "Expected object, received array",
			details: `{"formErrors":["Expected object, received array"],"fieldErrors":{}}`,
		},
		{
			// A top-level null is refused before validation runs.
			name: "invalid json", body: `null`, status: http.StatusBadRequest, code: "INVALID_JSON",
			message: "The request body is not valid JSON.",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp, raw := h.apiRaw(http.MethodPut, "/policy", adminToken, tc.body)
			if resp.StatusCode != tc.status {
				t.Fatalf("status = %d body %s", resp.StatusCode, raw)
			}
			body := string(raw)
			if !strings.Contains(body, `"code":"`+tc.code+`"`) {
				t.Fatalf("code missing: %s", body)
			}
			if !strings.Contains(body, `"message":"`+tc.message+`"`) {
				t.Fatalf("message = %s, want %q", body, tc.message)
			}
			if tc.details != "" && !strings.Contains(body, `"details":`+tc.details) {
				t.Fatalf("details = %s, want %s", body, tc.details)
			}
		})
	}
}

func TestPolicyUpdateIgnoresUnknownKeys(t *testing.T) {
	h := newHarness(t)
	h.reset()

	resp, raw := h.apiJSON(http.MethodPut, "/policy", adminToken,
		map[string]any{"enabled": false, "fallbackDirect": true, "unknown": 7})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d body %s", resp.StatusCode, raw)
	}
	if string(raw) != `{"policy":{"enabled":false,"fallbackDirect":true}}` {
		t.Fatalf("body = %s", raw)
	}
	if _, raw := h.apiJSON(http.MethodGet, "", adminToken, nil); !strings.Contains(string(raw),
		`"policy":{"enabled":false,"fallbackDirect":true}`) {
		t.Fatalf("list policy = %s", raw)
	}
}

func TestImportAndBulkValidation(t *testing.T) {
	h := newHarness(t)
	h.reset()

	_, raw := h.apiJSON(http.MethodPost, "/import", adminToken, map[string]any{"protocol": "http"})
	if !strings.Contains(string(raw), `"message":"text: Required"`) {
		t.Fatalf("import body = %s", raw)
	}
	_, raw = h.apiRaw(http.MethodPost, "/import", adminToken, `{"text":"h.test:1080","protocol":"ftp"}`)
	if !strings.Contains(string(raw),
		`"message":"protocol: Invalid enum value. Expected 'socks5' | 'http' | 'https', received 'ftp'"`) {
		t.Fatalf("protocol enum body = %s", raw)
	}
	_, raw = h.apiRaw(http.MethodPost, "/bulk", adminToken, `{"action":"nuke"}`)
	if !strings.Contains(string(raw),
		`"message":"action: Invalid enum value. Expected 'enable' | 'disable' | 'delete', received 'nuke'"`) {
		t.Fatalf("bulk action body = %s", raw)
	}
	_, raw = h.apiRaw(http.MethodPost, "/bulk", adminToken, `{"action":"enable","ids":"x"}`)
	if !strings.Contains(string(raw), `"message":"ids: Expected array, received string"`) {
		t.Fatalf("bulk ids body = %s", raw)
	}
	// all defaults to false, so an empty change list is a no-op, not an error.
	resp, raw := h.apiRaw(http.MethodPost, "/bulk", adminToken, `{"action":"enable"}`)
	if resp.StatusCode != http.StatusOK || string(raw) != `{"changed":0}` {
		t.Fatalf("bulk default body = %s, status %d", raw, resp.StatusCode)
	}
}

func TestUnknownProxyAndNoContentResponses(t *testing.T) {
	h := newHarness(t)
	h.reset()

	resp, raw := h.apiJSON(http.MethodPatch, "/missing-id", adminToken, map[string]any{"enabled": true})
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("patch unknown status = %d body %s", resp.StatusCode, raw)
	}
	if !strings.Contains(string(raw), `"code":"PROXY_NOT_FOUND"`) ||
		!strings.Contains(string(raw), `"message":"Proxy not found."`) {
		t.Fatalf("patch unknown body = %s", raw)
	}
	if resp, raw := h.apiJSON(http.MethodPost, "/missing-id/test", adminToken, map[string]any{"url": "https://example.com/"}); resp.StatusCode != http.StatusNotFound {
		t.Fatalf("test unknown status = %d body %s", resp.StatusCode, raw)
	}

	if resp, raw := h.apiJSON(http.MethodDelete, "/missing-id", adminToken, nil); resp.StatusCode != http.StatusNoContent {
		t.Fatalf("delete unknown status = %d body %s", resp.StatusCode, raw)
	}

	h.importProxies("h.test:1080", "socks5")
	proxy := h.list()[0]
	patch, body := h.apiJSON(http.MethodPatch, "/"+proxy.ID, adminToken, map[string]any{"enabled": false})
	if patch.StatusCode != http.StatusNoContent {
		t.Fatalf("patch status = %d body %s", patch.StatusCode, body)
	}
	if len(body) != 0 {
		t.Fatalf("204 body = %q", body)
	}
	if h.list()[0].Enabled {
		t.Fatal("proxy should be disabled")
	}
	if resp, _ := h.apiJSON(http.MethodDelete, "/"+proxy.ID, adminToken, nil); resp.StatusCode != http.StatusNoContent {
		t.Fatalf("delete status = %d", resp.StatusCode)
	}
	if len(h.list()) != 0 {
		t.Fatal("proxy should be deleted")
	}
}

func TestTestEndpointRejectsUnusableURLs(t *testing.T) {
	h := newHarness(t)
	h.reset()
	h.importProxies("127.0.0.1:"+strconv.Itoa(closedPort(t)), "socks5")
	proxy := h.list()[0]

	cases := []struct {
		name   string
		url    string
		status int
		code   string
	}{
		{name: "private", url: "http://127.0.0.1/", status: http.StatusBadRequest, code: "INVALID_TEST_URL"},
		{name: "credentials", url: "http://user:pass@example.com/", status: http.StatusBadRequest, code: "INVALID_TEST_URL"},
		{name: "non http", url: "ftp://example.com/", status: http.StatusBadRequest, code: "INVALID_TEST_URL"},
		{name: "not a url", url: "not-a-url", status: http.StatusBadRequest, code: "VALIDATION_ERROR"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp, raw := h.apiJSON(http.MethodPost, "/"+proxy.ID+"/test", adminToken, map[string]any{"url": tc.url})
			if resp.StatusCode != tc.status || !strings.Contains(string(raw), `"code":"`+tc.code+`"`) {
				t.Fatalf("status = %d body %s", resp.StatusCode, raw)
			}
		})
	}

	// A dead proxy still answers 200 with ok:false; the route does not fail.
	h.allowPrivateTestURLs()
	resp, raw := h.apiJSON(http.MethodPost, "/"+proxy.ID+"/test", adminToken,
		map[string]any{"url": "http://" + h.originHost + "/"})
	if resp.StatusCode != http.StatusOK || string(raw) != `{"ok":false}` {
		t.Fatalf("probe body = %s, status %d", raw, resp.StatusCode)
	}
	// The private-address guard is the caller's seam: it defaults to the ssrf check.
	if private, err := DefaultIsPrivateDestination(context.Background(), "http://127.0.0.1/"); err != nil || !private {
		t.Fatalf("loopback = %v, %v", private, err)
	}
	if private, err := DefaultIsPrivateDestination(context.Background(), "http://8.8.8.8/"); err != nil || private {
		t.Fatalf("public = %v, %v", private, err)
	}
}

func TestCheckRouteAndTrailingSlash(t *testing.T) {
	h := newHarness(t)
	socks := startSOCKS5(t)
	h.reset()
	h.importProxies("127.0.0.1:"+strconv.Itoa(socks.port()), "socks5")
	// The default check page is on the internet; point it at the local origin.
	h.svc.pol.CheckURL = "http://" + h.originHost + "/"

	resp, raw := h.apiJSON(http.MethodPost, "/check", adminToken, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("check status = %d body %s", resp.StatusCode, raw)
	}
	if !strings.HasPrefix(string(raw), `{"at":`) || !strings.HasSuffix(string(raw), `,"checked":1,"ok":1}`) {
		t.Fatalf("check body = %s", raw)
	}
	if _, list := h.apiJSON(http.MethodGet, "/", adminToken, nil); !strings.Contains(string(list), `"lastCheck":{"at":`) {
		t.Fatalf("list lastCheck = %s", list)
	}
	// Routing is not strict about the trailing slash, so both spellings reach
	// the list.
	if resp, _ := h.apiJSON(http.MethodGet, "", adminToken, nil); resp.StatusCode != http.StatusOK {
		t.Fatalf("collection status = %d", resp.StatusCode)
	}
	if resp, _ := h.apiJSON(http.MethodGet, "/", adminToken, nil); resp.StatusCode != http.StatusOK {
		t.Fatalf("trailing slash status = %d", resp.StatusCode)
	}
	// A sub-path of the collection is not the collection.
	if resp, _ := h.apiJSON(http.MethodGet, "/nope", adminToken, nil); resp.StatusCode != http.StatusNotFound &&
		resp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("unknown sub-path status = %d", resp.StatusCode)
	}
}

func TestProbeReportsStatusAndLatency(t *testing.T) {
	h := newHarness(t)
	socks := startSOCKS5(t)
	h.reset()
	h.importProxies("127.0.0.1:"+strconv.Itoa(socks.port()), "socks5")
	h.allowPrivateTestURLs()
	proxy := h.list()[0]

	resp, raw := h.apiJSON(http.MethodPost, "/"+proxy.ID+"/test", adminToken,
		map[string]any{"url": "http://" + h.originHost + "/"})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d body %s", resp.StatusCode, raw)
	}
	body := string(raw)
	if !strings.HasPrefix(body, `{"ok":true,"status":200,"latencyMs":`) {
		t.Fatalf("probe body = %s", body)
	}
	checked := h.list()[0]
	if checked.CheckedAt == nil || checked.CheckOk == nil || !*checked.CheckOk {
		t.Fatalf("probe should record the check: %+v", checked)
	}
}
