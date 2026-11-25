package proxies

// Proxy list import, ported from server/services/proxies/import.js. The accepted
// formats are the ones admins copy out of provider dashboards: a URL, host:port,
// host:port:user:pass, or the same separated by commas or tabs.

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/NeoLabs-Systems/neorouter/internal/httpserver"
	"github.com/google/uuid"
)

// Entry is one parsed proxy line.
type Entry struct {
	URL         string // normalized proxy URL, sealed at rest
	Endpoint    string // scheme://host, what the admin UI lists
	Fingerprint string // sha256 of URL, the uniqueness key
}

// Parse turns one import line into an Entry. protocol is the fallback scheme for
// a line without one (import.js defaults it to socks5). The error is
// ErrInvalidProxy for anything the JS parser rejects.
func Parse(line, protocol string) (Entry, error) {
	if protocol == "" {
		protocol = "socks5"
	}
	input := strings.TrimSpace(line)
	if !strings.Contains(input, "://") && !strings.Contains(input, "@") {
		var parts []string
		if strings.ContainsAny(input, ",\t") {
			for _, part := range strings.FieldsFunc(input, func(r rune) bool { return r == ',' || r == '\t' }) {
				parts = append(parts, strings.TrimSpace(part))
			}
		} else {
			parts = strings.Split(input, ":")
		}
		switch len(parts) {
		case 2:
			input = parts[0] + ":" + parts[1]
		case 4:
			input = encodeURIComponent(parts[2]) + ":" + encodeURIComponent(parts[3]) + "@" + parts[0] + ":" + parts[1]
		}
	}

	raw := input
	if !strings.Contains(input, "://") {
		raw = protocol + "://" + input
	}
	u, err := parseProxyURL(raw)
	if err != nil {
		return Entry{}, ErrInvalidProxy
	}
	switch u.Scheme {
	case "socks5", "socks5h", "http", "https":
	default:
		return Entry{}, ErrInvalidProxy
	}
	if strings.HasPrefix(u.Scheme, "socks") && u.Port == "" {
		return Entry{}, ErrInvalidProxy
	}
	if u.Port != "" {
		port, err := strconv.Atoi(u.Port)
		if err != nil || port < 1 || port > 65535 {
			return Entry{}, ErrInvalidProxy
		}
	}
	if u.Hostname == "" {
		return Entry{}, ErrInvalidProxy
	}
	if u.Path != "" && u.Path != "/" {
		return Entry{}, ErrInvalidProxy
	}
	if u.Search != "" || u.Hash != "" {
		return Entry{}, ErrInvalidProxy
	}
	if _, err := u.DecodedUser(); err != nil {
		return Entry{}, ErrInvalidProxy
	}
	if _, err := u.DecodedPass(); err != nil {
		return Entry{}, ErrInvalidProxy
	}

	// import.js clears the pathname; a special scheme then serializes it as "/".
	u.Path = ""
	if specialScheme(u.Scheme) {
		u.Path = "/"
	}
	url := u.String()
	return Entry{URL: url, Endpoint: u.Scheme + "://" + u.Host, Fingerprint: fingerprint(url)}, nil
}

// ImportResult is the body of POST /api/v1/admin/proxies/import.
type ImportResult struct {
	Added        int   `json:"added"`
	Duplicates   int   `json:"duplicates"`
	InvalidLines []int `json:"invalidLines"`
}

// ImportLines stores each usable line of text, skipping duplicates by
// fingerprint. Line numbers in InvalidLines are 1-based and count the comment
// and blank lines too, exactly as pool.js indexes them.
func (s *Service) ImportLines(ctx context.Context, text, protocol string) (ImportResult, error) {
	p := s.pol
	lines := strings.Split(strings.TrimRight(text, trimSpaceRunes), "\n")
	if len(lines) > p.MaxImport {
		return ImportResult{}, fmt.Errorf("proxies: import: too many lines: %w", httpserver.New(400, "TOO_MANY_PROXIES",
			fmt.Sprintf("Import at most %d lines at once.", p.MaxImport)))
	}

	result := ImportResult{InvalidLines: []int{}}
	tx, err := s.DB.W.BeginTx(ctx, nil)
	if err != nil {
		return ImportResult{}, fmt.Errorf("proxies: import: begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	insert, err := tx.PrepareContext(ctx,
		`INSERT OR IGNORE INTO outbound_proxies (id, fingerprint, endpoint, secret_encrypted) VALUES (?, ?, ?, ?)`)
	if err != nil {
		return ImportResult{}, fmt.Errorf("proxies: import: prepare: %w", err)
	}
	defer func() { _ = insert.Close() }()

	for index, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		entry, err := Parse(line, protocol)
		if err != nil {
			result.InvalidLines = append(result.InvalidLines, index+1)
			continue
		}
		sealed, err := s.Sealer.Seal(entry.URL)
		if err != nil {
			return ImportResult{}, fmt.Errorf("proxies: import: seal proxy: %w", err)
		}
		res, err := insert.ExecContext(ctx, uuid.NewString(), entry.Fingerprint, entry.Endpoint, sealed)
		if err != nil {
			return ImportResult{}, fmt.Errorf("proxies: import: insert: %w", err)
		}
		changes, err := res.RowsAffected()
		if err != nil {
			return ImportResult{}, fmt.Errorf("proxies: import: rows affected: %w", err)
		}
		if changes > 0 {
			result.Added++
		} else {
			result.Duplicates++
		}
	}
	if err := tx.Commit(); err != nil {
		return ImportResult{}, fmt.Errorf("proxies: import: commit: %w", err)
	}
	return result, nil
}

// trimSpaceRunes is what String.prototype.trimEnd removes: whitespace, including
// the Unicode spaces Go's unicode.IsSpace does not cover.
const trimSpaceRunes = " \t\n\v\f\r\u0085\u00a0\u1680\u2000\u2001\u2002\u2003\u2004\u2005\u2006\u2007\u2008\u2009\u200a\u2028\u2029\u202f\u205f\u3000\ufeff"
