package providers

import (
	"context"
	"net"
	"net/netip"
	"net/url"
	"strings"
)

// privateRanges are the local and private ranges net_guard.js blocks. Only the
// instance admin may point a provider at these (Ollama, LM Studio, a LAN box);
// everyone else is limited to public hosts, so an account's URL cannot reach
// the server's own network.
var privateRanges = mustPrefixes(
	"0.0.0.0/8",      // "this" network
	"10.0.0.0/8",     // private
	"100.64.0.0/10",  // carrier-grade NAT
	"127.0.0.0/8",    // loopback
	"169.254.0.0/16", // link-local, including cloud metadata
	"172.16.0.0/12",  // private
	"192.0.0.0/24",   // IETF protocol assignments
	"192.168.0.0/16", // private
	"198.18.0.0/15",  // benchmarking
	"224.0.0.0/4",    // multicast
	"240.0.0.0/4",    // reserved and broadcast
	"::/128",         // unspecified
	"::1/128",        // loopback
	"fc00::/7",       // unique local
	"fe80::/10",      // link-local
	"ff00::/8",       // multicast
)

func mustPrefixes(list ...string) []netip.Prefix {
	out := make([]netip.Prefix, len(list))
	for i, text := range list {
		out[i] = netip.MustParsePrefix(text)
	}
	return out
}

// IsPrivateAddress reports whether an address is local, private or otherwise
// not a public internet host. A value that is not an IP address counts as
// private, as net.isIP's zero answer does in net_guard.js.
func IsPrivateAddress(address string) bool {
	trimmed := strings.TrimPrefix(strings.TrimSuffix(address, "]"), "[")
	// An IPv4 address written as ::ffff:1.2.3.4 reaches the IPv4 rules.
	if mapped, ok := strings.CutPrefix(strings.ToLower(trimmed), "::ffff:"); ok && strings.Contains(mapped, ".") {
		if addr, err := netip.ParseAddr(mapped); err == nil {
			return checkPrivate(addr.Unmap())
		}
	}
	addr, err := netip.ParseAddr(trimmed)
	if err != nil {
		return true
	}
	return checkPrivate(addr.Unmap())
}

func checkPrivate(addr netip.Addr) bool {
	for _, prefix := range privateRanges {
		if prefix.Contains(addr) {
			return true
		}
	}
	return false
}

// Resolver resolves a host to the addresses it points at. nil uses the system
// resolver; tests pass a stub.
type Resolver func(ctx context.Context, host string) ([]netip.Addr, error)

// IsPrivateDestination resolves a URL's host and reports whether any address it
// points at is local or private. A host that does not resolve is not private:
// the request fails as a network error instead. A URL that does not parse is
// private.
func IsPrivateDestination(ctx context.Context, rawURL string, resolve Resolver) bool {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return true
	}
	host := parsed.Hostname()
	if host == "" {
		return true
	}
	if addr, err := netip.ParseAddr(host); err == nil {
		return checkPrivate(addr.Unmap())
	}
	if resolve == nil {
		resolve = lookupHost
	}
	addresses, err := resolve(ctx, host)
	if err != nil {
		return false
	}
	for _, addr := range addresses {
		if checkPrivate(addr.Unmap()) {
			return true
		}
	}
	return false
}

// lookupHost is the system resolver, as dns.lookup(host, { all: true }) is for
// net_guard.js.
func lookupHost(ctx context.Context, host string) ([]netip.Addr, error) {
	return net.DefaultResolver.LookupNetIP(ctx, "ip", host)
}
