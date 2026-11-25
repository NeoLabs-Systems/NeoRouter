package pools

// A member without a public address is reached through its pool's host. Its
// address names both: relay://<pool id>.<member id>. Older NeoRouters only
// accept http(s) addresses, so they simply leave such members out
// (relay/address.js).

import (
	"regexp"
	"strings"
)

const relayUUIDSource = `[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}`

var (
	relayUUID      = regexp.MustCompile(`(?i)^` + relayUUIDSource + `$`)
	relayAddressRe = regexp.MustCompile(`(?i)^relay://(` + relayUUIDSource + `)\.(` + relayUUIDSource + `)(/.*)?$`)
)

// relayAddress is a relay://<pool>.<member>(/path) address.
type relayAddress struct {
	PoolID   string
	MemberID string
	Path     string
}

// formatRelayAddress is the relay address of one member in one pool.
func formatRelayAddress(poolID, memberID string) string {
	return "relay://" + strings.ToLower(poolID) + "." + strings.ToLower(memberID)
}

// parseRelayAddress reads a relay address (with an optional path), reporting
// false when the value is not one.
func parseRelayAddress(value string) (relayAddress, bool) {
	match := relayAddressRe.FindStringSubmatch(strings.TrimSpace(value))
	if match == nil {
		return relayAddress{}, false
	}
	path := match[3]
	if path == "" {
		path = "/"
	}
	return relayAddress{
		PoolID:   strings.ToLower(match[1]),
		MemberID: strings.ToLower(match[2]),
		Path:     path,
	}, true
}

// isRelayAddress reports whether a value is a relay address.
func isRelayAddress(value string) bool {
	_, ok := parseRelayAddress(value)
	return ok
}
