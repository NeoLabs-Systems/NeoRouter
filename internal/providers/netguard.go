package providers

import (
	"context"
	"net"
	"net/netip"
	"net/url"
	"strings"

	"github.com/NeoLabs-Systems/neolabs-go/ssrf"
)

// IsPrivateAddress reports whether an address is local, private or otherwise
// not a public internet host. A value that is not an IP address counts as
// private.
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

// checkPrivate uses the ranges the guarded client refuses to dial, so the check
// before a call and the one at dial time agree. Only the instance admin may
// point a provider at such an address (Ollama, LM Studio, a LAN box).
func checkPrivate(addr netip.Addr) bool { return ssrf.CheckAddr(addr) != nil }

// Resolver resolves a host to the addresses it points at. nil uses the system
// resolver; tests pass a stub.
type Resolver func(ctx context.Context, host string) ([]netip.Addr, error)

// CheckDestination resolves a URL's host and reports whether any address it
// points at is local or private. A URL that does not parse is private. The error
// is a failed lookup: nothing is known about such a host, so a public-only
// caller must refuse it.
func CheckDestination(ctx context.Context, rawURL string, resolve Resolver) (bool, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return true, nil
	}
	host := parsed.Hostname()
	if host == "" {
		return true, nil
	}
	if addr, err := netip.ParseAddr(host); err == nil {
		return checkPrivate(addr.Unmap()), nil
	}
	if resolve == nil {
		resolve = lookupHost
	}
	addresses, err := resolve(ctx, host)
	if err != nil {
		return false, err
	}
	for _, addr := range addresses {
		if checkPrivate(addr.Unmap()) {
			return true, nil
		}
	}
	return false, nil
}

// IsPrivateDestination is CheckDestination for callers that only pick a route:
// a host that does not resolve fails there as a network error instead.
func IsPrivateDestination(ctx context.Context, rawURL string, resolve Resolver) bool {
	private, _ := CheckDestination(ctx, rawURL, resolve)
	return private
}

// lookupHost is the system resolver, returning every address.
func lookupHost(ctx context.Context, host string) ([]netip.Addr, error) {
	return net.DefaultResolver.LookupNetIP(ctx, "ip", host)
}
