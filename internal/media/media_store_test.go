package media

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/NeoLabs-Systems/neorouter/internal/config"
)

func TestMediaStoreRoundTrip(t *testing.T) {
	store := newMediaStore()
	now := time.UnixMilli(1_700_000_000_000)
	store.now = func() int64 { return now.UnixMilli() }
	id := store.put([]byte("png"), "image/png")
	if len(id) != 43 {
		t.Fatalf("id %q is 32 bytes base64url", id)
	}
	item := store.get(id)
	if item == nil || string(item.data) != "png" || item.mime != "image/png" {
		t.Fatalf("item = %+v", item)
	}
	if store.get("nope") != nil {
		t.Fatal("an unknown id is nil")
	}
	now = now.Add(time.Hour)
	if store.get(id) != nil {
		t.Fatal("an entry is gone after its hour")
	}
}

func TestMediaStoreSweepsOldestFirst(t *testing.T) {
	store := newMediaStore()
	now := time.UnixMilli(1_700_000_000_000)
	store.now = func() int64 { return now.UnixMilli() }
	old := store.put([]byte("old"), "image/png")
	now = now.Add(59 * time.Minute)
	fresh := store.put([]byte("fresh"), "image/png")
	if store.get(old) == nil {
		t.Fatal("a live entry is kept")
	}
	now = now.Add(2 * time.Minute)
	kept := store.put([]byte("later"), "image/png")
	if store.get(old) != nil {
		t.Fatal("an expired entry is swept when the store is written")
	}
	if store.get(fresh) == nil || store.get(kept) == nil {
		t.Fatal("live entries are kept")
	}
}

func TestSafeContentType(t *testing.T) {
	cases := []struct{ mime, want string }{
		{"image/png", "image/png"},
		{"image/jpeg; charset=binary", "image/jpeg"},
		{"IMAGE/WEBP", "image/webp"},
		{"audio/mpeg", "audio/mpeg"},
		{"video/mp4", "video/mp4"},
		{"video/quicktime", "video/quicktime"},
		{"text/html", ""},
		{"image/svg+xml", ""},
		{"text/javascript", ""},
		{"", ""},
	}
	for _, testCase := range cases {
		if got := safeContentType(testCase.mime); got != testCase.want {
			t.Errorf("safeContentType(%q) = %q, want %q", testCase.mime, got, testCase.want)
		}
	}
}

func TestServeHeaders(t *testing.T) {
	recorder := httptest.NewRecorder()
	serveHeaders(recorder, "text/html")
	header := recorder.Header()
	if header.Get("Content-Type") != "application/octet-stream" {
		t.Fatalf("Content-Type = %q", header.Get("Content-Type"))
	}
	if header.Get("Content-Disposition") != "attachment" {
		t.Fatalf("a page is a download")
	}
	if header.Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("X-Content-Type-Options = %q", header.Get("X-Content-Type-Options"))
	}
	if !strings.Contains(header.Get("Content-Security-Policy"), "sandbox") {
		t.Fatalf("Content-Security-Policy = %q", header.Get("Content-Security-Policy"))
	}
	image := httptest.NewRecorder()
	serveHeaders(image, "image/png")
	if image.Header().Get("Content-Type") != "image/png" {
		t.Fatalf("Content-Type = %q", image.Header().Get("Content-Type"))
	}
	if image.Header().Get("Content-Disposition") != "" {
		t.Fatal("media is shown, not downloaded")
	}
}

func TestPublicURL(t *testing.T) {
	service := New(Options{Config: func() config.Config { return config.Config{PublicURL: "https://router.example.com/"} }})
	request := httptest.NewRequest(http.MethodGet, "http://internal.local/v1/images/generations", nil)
	if got := service.publicURL(request, "abc"); got != "https://router.example.com/v1/media/abc" {
		t.Fatalf("url = %q", got)
	}
	service = New(Options{})
	request = httptest.NewRequest(http.MethodGet, "http://router.test/v1/images/generations", nil)
	if got := service.publicURL(request, "abc"); got != "http://router.test/v1/media/abc" {
		t.Fatalf("url = %q", got)
	}
	ssl := httptest.NewRequest(http.MethodGet, "http://router.test/v1/images/generations", nil)
	ssl.Header.Set("X-Forwarded-Proto", "https")
	if got := service.publicURL(ssl, "abc"); got != "https://router.test/v1/media/abc" {
		t.Fatalf("url = %q", got)
	}
}
