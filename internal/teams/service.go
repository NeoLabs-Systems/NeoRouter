package teams

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"
	"unicode/utf16"

	"github.com/NeoLabs-Systems/neolabs-go/dbx"
	"github.com/NeoLabs-Systems/neorouter/internal/config"
)

// ActorFunc reports the authenticated account of a request. The wiring passes
// auth.Actor, which reads what the session middleware put on the context.
type ActorFunc func(*http.Request) (string, bool)

// Service owns NeoRouter's teams: membership, invite links, the team audit
// trail and the X-NeoRouter-Space scope. Dependencies are fields, so the app
// builds it with a struct literal.
type Service struct {
	DB  *dbx.DB
	Cfg func() config.Config
	Log *slog.Logger

	// Actor reports the account a request is authenticated as. SpaceMiddleware
	// resolves the space with it; a nil Actor means no request is authenticated.
	Actor ActorFunc
}

func (s *Service) cfg() config.Config {
	if s.Cfg != nil {
		return s.Cfg()
	}
	return config.Config{}
}

func (s *Service) log() *slog.Logger {
	if s.Log != nil {
		return s.Log
	}
	return slog.Default()
}

// dbtx is the part of database/sql shared by a pool and a transaction, so the
// same query helpers serve both a plain read and a transactional step.
type dbtx interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// isoMillis formats t the way the Node server stores dates:
// new Date().toISOString(), always UTC with milliseconds.
func isoMillis(t time.Time) string {
	return t.UTC().Format("2006-01-02T15:04:05.000Z")
}

// parseISO reads a stored ISO timestamp, reporting false when it is not one.
func parseISO(value string) (time.Time, bool) {
	t, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Time{}, false
	}
	return t, true
}

// jsLen is the length JavaScript's String#length reports: UTF-16 code units,
// which is what the request validation bounds are measured in.
func jsLen(value string) int { return len(utf16.Encode([]rune(value))) }

// truncRunes cuts value to at most n characters, standing in for the JS
// String#slice(0, n) used on labels and display names.
func truncRunes(value string, n int) string {
	runes := []rune(value)
	if len(runes) <= n {
		return value
	}
	return string(runes[:n])
}

// nullString returns a nullable column value for an optional string.
func nullString(value *string) sql.NullString {
	if value == nil {
		return sql.NullString{}
	}
	return sql.NullString{String: *value, Valid: true}
}

// stringPtr returns the value of a nullable text column.
func stringPtr(value sql.NullString) *string {
	if !value.Valid {
		return nil
	}
	out := value.String
	return &out
}

// decodeStringArray reads a JSON array of strings, reporting false when the
// text is not a JSON array at all.
func decodeStringArray(raw string) ([]string, bool) {
	var values []any
	if err := json.Unmarshal([]byte(raw), &values); err != nil {
		return nil, false
	}
	out := make([]string, 0, len(values))
	for _, value := range values {
		if text, ok := value.(string); ok {
			out = append(out, text)
		}
	}
	return out, true
}

// encodeStringArray writes a JSON array, never null, the way JSON.stringify of
// an array does.
func encodeStringArray(values []string) string {
	if values == nil {
		values = []string{}
	}
	raw, err := json.Marshal(values)
	if err != nil {
		return "[]"
	}
	return string(raw)
}

// spaceActorID is the account a team endpoint acts for. Every team endpoint is
// own-space only, so the resolved space is always present.
func spaceActorID(r *http.Request) (string, error) {
	space, ok := SpaceFromContext(r.Context())
	if !ok {
		return "", fmt.Errorf("teams: no space on the request context")
	}
	return space.ActorID, nil
}

// publicOrigin is this server's address as people reach it: the configured
// public URL, else the request's own scheme and host, without a trailing slash.
func (s *Service) publicOrigin(r *http.Request) string {
	cfg := s.cfg()
	origin := cfg.PublicURL
	if origin == "" {
		scheme := "http"
		if r.TLS != nil {
			scheme = "https"
		}
		if cfg.TrustProxy {
			if proto := r.Header.Get("X-Forwarded-Proto"); proto != "" {
				scheme, _, _ = strings.Cut(proto, ",")
				scheme = strings.TrimSpace(scheme)
			}
		}
		origin = scheme + "://" + r.Host
	}
	return strings.TrimRight(origin, "/")
}
