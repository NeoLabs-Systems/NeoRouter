package media

import (
	"crypto/rand"
	"encoding/base64"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"
)

// Short-lived storage for generated media, so a client that asks for a URL gets
// one even when the provider returned raw bytes. Ids are random 32-byte tokens
// — the URL is the capability — entries expire after an hour and the store is
// capped in size. Nothing is written to disk.

const (
	mediaTTLMS    int64 = 60 * 60 * 1000
	mediaMaxBytes       = 256 * 1024 * 1024
)

// mediaItem is one generated file waiting to be collected.
type mediaItem struct {
	data      []byte
	mime      string
	expiresAt int64
}

// mediaStore keeps generated media in memory, oldest first.
type mediaStore struct {
	mu    sync.Mutex
	items map[string]*mediaItem
	order []string
	total int
	newID func() string
	// now is the clock the expiry runs on (the service's clock in production).
	now func() int64
}

func newMediaStore() *mediaStore {
	return &mediaStore{items: map[string]*mediaItem{}, newID: randomMediaID, now: nowMillis}
}

// nowMillis is the wall clock in epoch milliseconds.
func nowMillis() int64 { return time.Now().UnixMilli() }

// randomMediaID is 32 random bytes, base64url as crypto.randomBytes(32)
// .toString('base64url') renders them.
func randomMediaID() string {
	buffer := make([]byte, 32)
	if _, err := rand.Read(buffer); err != nil {
		return ""
	}
	return base64.RawURLEncoding.EncodeToString(buffer)
}

// sweep drops expired entries from the oldest end, and stops at the first live
// one while the store is within its size cap.
func (s *mediaStore) sweep(now int64) {
	for index, id := range s.order {
		item, ok := s.items[id]
		if ok && item.expiresAt > now && s.total <= mediaMaxBytes {
			s.order = append([]string(nil), s.order[index:]...)
			return
		}
		if ok {
			delete(s.items, id)
			s.total -= len(item.data)
		}
	}
	s.order = nil
}

// put stores one file and returns the id its URL carries.
func (s *mediaStore) put(data []byte, mime string) string {
	now := s.now()
	id := s.newID()
	if id == "" {
		return ""
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sweep(now)
	s.items[id] = &mediaItem{data: data, mime: mime, expiresAt: now + mediaTTLMS}
	s.order = append(s.order, id)
	s.total += len(data)
	s.sweep(now)
	return id
}

// get reads one file, or nil when it never existed or has expired.
func (s *mediaStore) get(id string) *mediaItem {
	s.mu.Lock()
	defer s.mu.Unlock()
	item, ok := s.items[id]
	if !ok || item.expiresAt <= s.now() {
		return nil
	}
	return item
}

// safeMediaType is the content type a generated file may be served as: the
// provider picks it and these links open on the app's own origin, so only plain
// image, audio and video types are served as such. Anything else (HTML, SVG,
// scripts) is a download.
var safeMediaType = regexp.MustCompile(`^(image/(png|jpeg|webp|gif|avif)|audio/[a-z0-9.+-]+|video/(mp4|webm|quicktime))$`)

// safeContentType is the mime a link may answer with, "" when it may not.
func safeContentType(mime string) string {
	kind := strings.ToLower(strings.TrimSpace(strings.SplitN(mime, ";", 2)[0]))
	if safeMediaType.MatchString(kind) {
		return kind
	}
	return ""
}

// serveHeaders writes the headers every generated-media response carries.
func serveHeaders(w http.ResponseWriter, mime string) {
	kind := safeContentType(mime)
	if kind == "" {
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Disposition", "attachment")
	} else {
		w.Header().Set("Content-Type", kind)
	}
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; sandbox")
}

// publicURL is the absolute link a client fetches generated media from: the
// configured public URL, else the request's own origin.
func (s *Service) publicURL(r *http.Request, id string) string {
	origin := s.settings().PublicURL
	if origin == "" {
		scheme := "http"
		if r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https") {
			scheme = "https"
		}
		origin = scheme + "://" + r.Host
	}
	return strings.TrimRight(origin, "/") + "/v1/media/" + id
}
