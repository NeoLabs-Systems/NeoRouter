package pools

// The invite page's markup is part of the invite experience: these tests compare it
// byte for byte with the page the Node server produced (testdata/pool_join_*.html,
// rendered from server/routes/pool_join.js).

import (
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
)

// TestJoinPageMarkup compares the rendered page with the Node page.
func TestJoinPageMarkup(t *testing.T) {
	cases := []struct {
		name   string
		file   string
		lang   string
		invite *joinInvite
	}{
		{"english", "pool_join_en.html", "en", &joinInvite{Pool: `Quiz <Night> & "Co"`, Host: "alex & co", Members: 1}},
		{"german", "pool_join_de.html", "de", &joinInvite{Pool: `Quiz <Night> & "Co"`, Host: "alex & co", Members: 1}},
		{"invalid", "pool_join_invalid.html", "en", nil},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			want, err := os.ReadFile("testdata/" + testCase.file)
			if err != nil {
				t.Fatalf("read the Node page: %v", err)
			}
			got := renderJoinPage(testCase.lang, "https://router.example.com", testCase.invite)
			if got != string(want) {
				t.Fatalf("the page differs from the Node page:\n--- got ---\n%s\n--- want ---\n%s", got, want)
			}
		})
	}
}

// TestJoinLanguage picks the page's language from ?lang=, else Accept-Language.
func TestJoinLanguage(t *testing.T) {
	cases := []struct{ lang, header, want string }{
		{"de", "", "de"},
		{"en", "de-DE", "en"},
		{"", "de", "de"},
		{"", "de-AT,de;q=0.9", "de"},
		{"", "en-US", "en"},
		{"", "denmark", "en"},
		{"", "", "en"},
	}
	for _, testCase := range cases {
		if got := joinLanguage(testCase.lang, testCase.header); got != testCase.want {
			t.Fatalf("joinLanguage(%q, %q) = %q, want %q", testCase.lang, testCase.header, got, testCase.want)
		}
	}
}

// TestInvitePageServesTheLink serves the page the way the invite link does.
func TestInvitePageServesTheLink(t *testing.T) {
	harness := newHarness(t)
	defer harness.close()
	alex := harness.register("alex")
	created := decoded(t, harness.api(alex, "POST", "/pools", map[string]any{
		"name": `Quiz <Night>`, "selfName": "alex", "sharedAccountIds": []string{},
	}))
	link := harness.invite(alex, created["poolId"].(string))
	token := link[len(link)-32:]

	page := harness.getPage(t, "/pool/join?pool="+token, "en-US")
	if !strings.Contains(page, `<meta property="og:title" content="Invite to the pool &quot;Quiz &lt;Night&gt;&quot;">`) {
		t.Fatalf("the pool name is not in the link preview: %s", page)
	}
	if !strings.Contains(page, "join-pool=") {
		t.Fatal("the page does not forward to the own NeoRouter with the invite")
	}
	if !strings.Contains(harness.getPage(t, "/pool/join?pool="+token+"&lang=de", ""), "Einladung in den Pool") {
		t.Fatal("the German page is missing")
	}
	invalid := harness.getPage(t, "/pool/join?pool="+strings.Repeat("x", 32), "en-US")
	if !strings.Contains(invalid, "no longer valid") {
		t.Fatalf("an unused token is not reported as invalid: %s", invalid)
	}
}

// getPage fetches one page from the test server.
func (h *harness) getPage(t *testing.T, path, acceptLanguage string) string {
	t.Helper()
	code, body, _ := h.rawGet(t, path, acceptLanguage)
	if code != http.StatusOK {
		t.Fatalf("GET %s: %d", path, code)
	}
	return body
}

// rawGet fetches one page, recording what the browser saw.
func (h *harness) rawGet(t *testing.T, path, acceptLanguage string) (int, string, http.Header) {
	t.Helper()
	request, err := http.NewRequest(http.MethodGet, h.base()+path, nil)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	if acceptLanguage != "" {
		request.Header.Set("Accept-Language", acceptLanguage)
	}
	response, err := h.server.Client().Do(request)
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return response.StatusCode, string(raw), response.Header
}
