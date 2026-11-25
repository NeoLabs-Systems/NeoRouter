package proxies

// Import parsing pinned against Node's own output. Every expectation below was
// produced by running server/services/proxies/import.js, so a drift in the URL
// normalization, the endpoint or the accepted formats fails here.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strings"
	"testing"
)

func TestParseMatchesNodeURLNormalization(t *testing.T) {
	cases := []struct {
		name     string
		line     string
		protocol string
		wantURL  string
		wantEnd  string
		wantErr  bool
	}{
		{name: "colon separated credentials", line: "host.test:1080:user:secret",
			wantURL: "socks5://user:secret@host.test:1080", wantEnd: "socks5://host.test:1080"},
		{name: "socks5 url", line: "socks5://user:secret@host.test:1080",
			wantURL: "socks5://user:secret@host.test:1080", wantEnd: "socks5://host.test:1080"},
		{name: "comma separated", line: "host2.test,1080,user,pass",
			wantURL: "socks5://user:pass@host2.test:1080", wantEnd: "socks5://host2.test:1080"},
		{name: "tab separated", line: "h.test\t1080\tu\tp",
			wantURL: "socks5://u:p@h.test:1080", wantEnd: "socks5://h.test:1080"},
		{name: "comma separated with spaces", line: "h.test,1080,us er,pa ss",
			wantURL: "socks5://us%20er:pa%20ss@h.test:1080", wantEnd: "socks5://h.test:1080"},
		{name: "credentials without scheme", line: "user:secret@host3.test:1080",
			wantURL: "socks5://user:secret@host3.test:1080", wantEnd: "socks5://host3.test:1080"},
		{name: "ipv6 literal", line: "socks5://[::1]:1080",
			wantURL: "socks5://[::1]:1080", wantEnd: "socks5://[::1]:1080"},
		{name: "ipv6 with root path", line: "socks5://[::1]:1080/",
			wantURL: "socks5://[::1]:1080", wantEnd: "socks5://[::1]:1080"},
		{name: "upper case special scheme", line: "HTTP://User:secret@Host.Test:8080/",
			wantURL: "http://User:secret@host.test:8080/", wantEnd: "http://host.test:8080"},
		{name: "https default port dropped", line: "https://user:pass@host.test:443",
			wantURL: "https://user:pass@host.test/", wantEnd: "https://host.test"},
		{name: "http default port dropped", line: "http://host.test:80",
			wantURL: "http://host.test/", wantEnd: "http://host.test"},
		{name: "opaque scheme keeps root path out", line: "socks5://h.test:1/",
			wantURL: "socks5://h.test:1", wantEnd: "socks5://h.test:1"},
		{name: "socks5h kept", line: "socks5h://h.test:1080",
			wantURL: "socks5h://h.test:1080", wantEnd: "socks5h://h.test:1080"},
		{name: "ipv4 host:port", line: "10.9.0.1:1080",
			wantURL: "socks5://10.9.0.1:1080", wantEnd: "socks5://10.9.0.1:1080"},
		{name: "host:port", line: "host.test:1080",
			wantURL: "socks5://host.test:1080", wantEnd: "socks5://host.test:1080"},
		{name: "surrounding spaces", line: " socks5://user:secret@host.test:1080 ",
			wantURL: "socks5://user:secret@host.test:1080", wantEnd: "socks5://host.test:1080"},
		{name: "user without password", line: "socks5://user@h.test:1080",
			wantURL: "socks5://user@h.test:1080", wantEnd: "socks5://h.test:1080"},
		{name: "space in userinfo", line: "socks5://us er@h.test:1080",
			wantURL: "socks5://us%20er@h.test:1080", wantEnd: "socks5://h.test:1080"},
		{name: "userinfo encodes reserved characters", line: "socks5://u:p;q=r@h.test:1080",
			wantURL: "socks5://u:p%3Bq%3Dr@h.test:1080", wantEnd: "socks5://h.test:1080"},
		{name: "existing escapes kept", line: "socks5://u:%41%42@h.test:1080",
			wantURL: "socks5://u:%41%42@h.test:1080", wantEnd: "socks5://h.test:1080"},
		{name: "opaque scheme keeps host case", line: "socks5://H.TEST:1080",
			wantURL: "socks5://H.TEST:1080", wantEnd: "socks5://H.TEST:1080"},
		{name: "special scheme lowercases host", line: "http://u:p@H.TEST:3128/",
			wantURL: "http://u:p@h.test:3128/", wantEnd: "http://h.test:3128"},
		{name: "http protocol fallback", line: "h.test:3128", protocol: "http",
			wantURL: "http://h.test:3128/", wantEnd: "http://h.test:3128"},
		{name: "http protocol with credentials", line: "h.test:3128:u:p", protocol: "http",
			wantURL: "http://u:p@h.test:3128/", wantEnd: "http://h.test:3128"},
		{name: "http scheme overrides protocol", line: "http://u:p@h.test:1080", protocol: "http",
			wantURL: "http://u:p@h.test:1080/", wantEnd: "http://h.test:1080"},

		{name: "no port", line: "not-a-proxy", wantErr: true},
		{name: "unsupported scheme", line: "ftp://host.test:80", wantErr: true},
		{name: "path", line: "http://host.test:1080/path", wantErr: true},
		{name: "query", line: "http://host.test:1080?x=1", wantErr: true},
		{name: "fragment", line: "http://host.test:1080#x", wantErr: true},
		{name: "bad escape in userinfo", line: "socks5://user:%zz@host.test:1080", wantErr: true},
		{name: "port zero", line: "socks5://h.test:0", wantErr: true},
		{name: "port out of range", line: "socks5://h.test:99999", wantErr: true},
		{name: "non numeric port", line: "socks5://h.test:abc", wantErr: true},
		{name: "bare host", line: "host.test", wantErr: true},
		{name: "bare ipv6", line: "::1:1080", wantErr: true},
		{name: "empty authority", line: "socks5://", wantErr: true},
		{name: "comma list with an at sign", line: "host.test,1080,us er,p@ss", wantErr: true},
		{name: "socks needs a port", line: "socks5://h.test", wantErr: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			entry, err := Parse(tc.line, tc.protocol)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("Parse(%q) = %+v, want an error", tc.line, entry)
				}
				return
			}
			if err != nil {
				t.Fatalf("Parse(%q) error: %v", tc.line, err)
			}
			if entry.URL != tc.wantURL {
				t.Fatalf("url = %q, want %q", entry.URL, tc.wantURL)
			}
			if entry.Endpoint != tc.wantEnd {
				t.Fatalf("endpoint = %q, want %q", entry.Endpoint, tc.wantEnd)
			}
			sum := sha256.Sum256([]byte(entry.URL))
			if want := hex.EncodeToString(sum[:]); entry.Fingerprint != want {
				t.Fatalf("fingerprint = %q, want sha256(url) = %q", entry.Fingerprint, want)
			}
		})
	}
}

func TestImportLinesSkipsCommentsAndBlanks(t *testing.T) {
	h := newHarness(t)
	h.reset()

	// A trailing newline is trimmed first, so it never counts as a line.
	result := h.importProxies("# comment\n\nh1.test:1080\n  \nh2.test:1080\n", "socks5")
	if result.Added != 2 || result.Duplicates != 0 || len(result.InvalidLines) != 0 {
		t.Fatalf("result = %+v", result)
	}
	again := h.importProxies("h1.test:1080\nh2.test:1080", "socks5")
	if again.Added != 0 || again.Duplicates != 2 {
		t.Fatalf("repeat import = %+v", again)
	}
	// CRLF lists are what Windows admins paste; the line numbers stay the same.
	crlf := h.importProxies("h3.test:1080\r\nh4.test:1080\r\nbroken\r\n", "socks5")
	if crlf.Added != 2 || crlf.Duplicates != 0 || len(crlf.InvalidLines) != 1 || crlf.InvalidLines[0] != 3 {
		t.Fatalf("crlf import = %+v", crlf)
	}
}

func TestImportRejectsMoreThanTheLineLimit(t *testing.T) {
	h := newHarness(t)
	h.reset()

	text := strings.Repeat("h.test:1080\n", h.svc.Policy().MaxImport+1)
	_, err := h.svc.ImportLines(context.Background(), text, "socks5")
	if err == nil {
		t.Fatal("import over the limit should fail")
	}
	resp, raw := h.apiJSON(http.MethodPost, "/import", adminToken, map[string]any{"text": text})
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d body %s", resp.StatusCode, raw)
	}
	if !strings.Contains(string(raw), `"code":"TOO_MANY_PROXIES"`) ||
		!strings.Contains(string(raw), `"message":"Import at most 5000 lines at once."`) {
		t.Fatalf("body = %s", raw)
	}
}
