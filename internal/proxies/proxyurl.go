package proxies

// Proxy URL parsing. The stored value is the WHATWG-normalized URL, so the
// stored value, the visible endpoint and the dedupe fingerprint all depend on
// that normalization. This file reproduces the parts of it the proxy list can
// contain: a scheme, an optional userinfo, a host (name, IPv4 or bracketed
// IPv6) with an optional port, and an empty or root path.

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// ProxyURL is a parsed outbound proxy URL. Credentials stay percent-encoded, as
// they appear in the URL.
type ProxyURL struct {
	Scheme   string // lower-case, without the colon
	Username string // percent-encoded userinfo user
	Password string // percent-encoded userinfo password
	Host     string // host[:port]; IPv6 literals keep their brackets
	Hostname string // host without the port; IPv6 literals keep their brackets
	Port     string // empty when the URL has no port
	Path     string // "" or "/" (anything else is rejected by Parse)
	Search   string // raw "?..." (always rejected by Parse when non-empty)
	Hash     string // raw "#..." (always rejected by Parse when non-empty)
}

// specialScheme reports whether a scheme is one of WHATWG's "special" schemes.
// Special schemes lower-case their host, default their path to "/" and drop a
// default port; opaque schemes such as socks5: do none of that, which is why
// socks5://H.TEST:1080 keeps its case while http://H.TEST:80 does not.
func specialScheme(scheme string) bool {
	switch scheme {
	case "http", "https", "ws", "wss", "ftp", "file":
		return true
	}
	return false
}

func defaultPort(scheme string) string {
	switch scheme {
	case "http", "ws":
		return "80"
	case "https", "wss":
		return "443"
	case "ftp":
		return "21"
	}
	return ""
}

// DialAddr is the proxy's dial address, defaulting the port for http(s).
func (u ProxyURL) DialAddr() string {
	host := strings.TrimSuffix(strings.TrimPrefix(u.Hostname, "["), "]")
	port := u.Port
	if port == "" {
		port = defaultPort(u.Scheme)
	}
	return joinHostPort(host, port)
}

// DecodedUser returns the user with its percent escapes resolved.
func (u ProxyURL) DecodedUser() (string, error) { return percentDecode(u.Username) }

// DecodedPass returns the password with its percent escapes resolved.
func (u ProxyURL) DecodedPass() (string, error) {
	if u.Password == "" {
		return "", nil
	}
	return percentDecode(u.Password)
}

// String rebuilds the URL the way `new URL(...).toString()` makes it: lower-case
// scheme, normalized userinfo escaping, host with its port, and a path.
func (u ProxyURL) String() string {
	var b strings.Builder
	b.WriteString(u.Scheme)
	b.WriteString("://")
	if u.Username != "" || u.Password != "" {
		b.WriteString(u.Username)
		if u.Password != "" {
			b.WriteByte(':')
			b.WriteString(u.Password)
		}
		b.WriteByte('@')
	}
	b.WriteString(u.Host)
	b.WriteString(u.Path)
	b.WriteString(u.Search)
	b.WriteString(u.Hash)
	return b.String()
}

// ErrInvalidProxy is returned for a line that is not a usable proxy URL. It is
// the invalid-proxy error.
var ErrInvalidProxy = errors.New("Invalid proxy")

// parseProxyURL parses raw as a WHATWG URL would, accepting only what a proxy
// URL can hold. The caller decides which schemes are supported.
func parseProxyURL(raw string) (ProxyURL, error) {
	var u ProxyURL
	sep := strings.Index(raw, "://")
	if sep <= 0 {
		return u, ErrInvalidProxy
	}
	scheme := raw[:sep]
	for i := 0; i < len(scheme); i++ {
		c := scheme[i]
		alnum := c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9'
		if !alnum && c != '+' && c != '-' && c != '.' {
			return u, ErrInvalidProxy
		}
	}
	u.Scheme = strings.ToLower(scheme)
	rest := raw[sep+3:]

	// The authority ends at the first "/", "?" or "#".
	end := len(rest)
	for i := 0; i < len(rest); i++ {
		if rest[i] == '/' || rest[i] == '?' || rest[i] == '#' {
			end = i
			break
		}
	}
	authority, tail := rest[:end], rest[end:]

	if at := strings.LastIndex(authority, "@"); at >= 0 {
		userinfo, hostport := authority[:at], authority[at+1:]
		authority = hostport
		user, pass := userinfo, ""
		if colon := strings.Index(userinfo, ":"); colon >= 0 {
			user, pass = userinfo[:colon], userinfo[colon+1:]
		}
		if !validPercentEscapes(user) || !validPercentEscapes(pass) {
			return u, ErrInvalidProxy
		}
		u.Username = encodeUserInfo(user)
		u.Password = encodeUserInfo(pass)
	}

	hostport := authority
	port := ""
	bracketed := false
	if strings.HasPrefix(hostport, "[") {
		bracketed = true
		close := strings.Index(hostport, "]")
		if close < 0 {
			return u, ErrInvalidProxy
		}
		literal := hostport[1:close]
		if literal == "" || !strings.Contains(literal, ":") ||
			strings.IndexFunc(literal, func(r rune) bool {
				return !strings.ContainsRune("0123456789abcdefABCDEF:.", r)
			}) >= 0 {
			return u, ErrInvalidProxy
		}
		rest := hostport[close+1:]
		switch {
		case rest == "":
		case strings.HasPrefix(rest, ":"):
			port = rest[1:]
		default:
			return u, ErrInvalidProxy
		}
		hostport = hostport[:close+1]
	} else if colon := strings.LastIndex(hostport, ":"); colon >= 0 {
		hostport, port = hostport[:colon], hostport[colon+1:]
	}
	forbidden := ":#/?@[]\\^|<>\x00 \t"
	if bracketed {
		forbidden = "/?@\\^|<>\x00 \t"
	}
	if hostport == "" || strings.ContainsAny(hostport, forbidden) {
		return u, ErrInvalidProxy
	}

	if port != "" {
		for i := 0; i < len(port); i++ {
			if port[i] < '0' || port[i] > '9' {
				return u, ErrInvalidProxy
			}
		}
		n, err := strconv.Atoi(port)
		if err != nil || n > 65535 {
			return u, ErrInvalidProxy
		}
		port = strconv.Itoa(n) // WHATWG re-serializes the port without leading zeros
		if specialScheme(u.Scheme) && port == defaultPort(u.Scheme) {
			port = "" // a special scheme drops its own default port
		}
	}
	host := hostport
	if specialScheme(u.Scheme) {
		host = strings.ToLower(host)
	}
	if port != "" {
		host += ":" + port
	}

	path := tail
	switch {
	case strings.HasPrefix(tail, "#"):
		u.Hash, path = tail, ""
	case strings.IndexByte(tail, '#') >= 0:
		cut := strings.IndexByte(tail, '#')
		path, u.Hash = tail[:cut], tail[cut:]
	}
	if cut := strings.IndexByte(path, '?'); cut >= 0 {
		u.Search, path = path[cut:], path[:cut]
	}

	u.Host = host
	u.Hostname = hostport
	u.Port = port
	u.Path = path
	return u, nil
}

func joinHostPort(host, port string) string {
	if strings.Contains(host, ":") {
		return "[" + host + "]:" + port
	}
	return host + ":" + port
}

// userInfoSafe reports whether c survives userinfo serialization unchanged:
// unreserved characters and the sub-delimiters other than ";" and "=", which is
// the WHATWG userinfo percent-encode set.
func userInfoSafe(c byte) bool {
	switch {
	case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		return true
	case c == '-', c == '.', c == '_', c == '~':
		return true
	case c == '!', c == '$', c == '&', c == '\'', c == '(', c == ')', c == '*', c == '+', c == ',':
		return true
	}
	return false
}

// encodeUserInfo percent-encodes a userinfo component the way the WHATWG URL
// serializer does, leaving existing valid escapes alone.
func encodeUserInfo(s string) string {
	needsEncoding := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if userInfoSafe(c) {
			continue
		}
		if c == '%' && i+2 < len(s) && isHex(s[i+1]) && isHex(s[i+2]) {
			i += 2
			continue
		}
		needsEncoding = true
		break
	}
	if !needsEncoding {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if userInfoSafe(c) {
			b.WriteByte(c)
			continue
		}
		if c == '%' && i+2 < len(s) && isHex(s[i+1]) && isHex(s[i+2]) {
			b.WriteString(s[i : i+3])
			i += 2
			continue
		}
		fmt.Fprintf(&b, "%%%02X", c)
	}
	return b.String()
}

func isHex(c byte) bool {
	return c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F'
}

// validPercentEscapes reports whether every "%" starts a valid escape; a line
// with a stray "%" in its credentials is invalid.
func validPercentEscapes(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] != '%' {
			continue
		}
		if i+2 >= len(s) || !isHex(s[i+1]) || !isHex(s[i+2]) {
			return false
		}
		i += 2
	}
	return true
}

// percentDecode resolves "%XX" escapes, failing where decodeURIComponent fails.
func percentDecode(s string) (string, error) {
	if !validPercentEscapes(s) {
		return "", ErrInvalidProxy
	}
	if !strings.ContainsRune(s, '%') {
		return s, nil
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] != '%' {
			b.WriteByte(s[i])
			continue
		}
		hi, lo := unhex(s[i+1]), unhex(s[i+2])
		b.WriteByte(hi<<4 | lo)
		i += 2
	}
	return b.String(), nil
}

func unhex(c byte) byte {
	switch {
	case c >= '0' && c <= '9':
		return c - '0'
	case c >= 'a' && c <= 'f':
		return c - 'a' + 10
	default:
		return c - 'A' + 10
	}
}

// encodeURIComponent builds "user:pass@host:port" from comma- or tab-separated
// lists.
func encodeURIComponent(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r < 0x80 {
			c := byte(r)
			switch {
			case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
				b.WriteByte(c)
				continue
			case c == '-', c == '.', c == '_', c == '~':
				b.WriteByte(c)
				continue
			case c == '!', c == '*', c == '\'', c == '(', c == ')':
				b.WriteByte(c)
				continue
			}
			fmt.Fprintf(&b, "%%%02X", c)
			continue
		}
		for _, c := range []byte(string(r)) {
			fmt.Fprintf(&b, "%%%02X", c)
		}
	}
	return b.String()
}

func fingerprint(url string) string {
	sum := sha256.Sum256([]byte(url))
	return hex.EncodeToString(sum[:])
}
