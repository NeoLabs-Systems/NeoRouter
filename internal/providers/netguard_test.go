package providers

import (
	"context"
	"errors"
	"net/netip"
	"testing"
)

// TestIsPrivateAddress ports the ranges net_guard.js blocks.
func TestIsPrivateAddress(t *testing.T) {
	for _, address := range []string{
		"0.0.0.0", "0.1.2.3", "10.0.0.1", "10.255.255.255", "100.64.0.1", "127.0.0.1", "127.9.9.9",
		"169.254.169.254", "172.16.0.1", "172.31.255.255", "192.0.0.8", "192.168.1.1", "198.18.0.1",
		"198.19.255.255", "224.0.0.1", "240.0.0.1", "255.255.255.255",
		"::", "::1", "fc00::1", "fd12:3456::1", "fe80::1", "ff02::1",
		// An IPv4 address written as an IPv6 one follows the IPv4 rules.
		"::ffff:127.0.0.1", "::ffff:10.1.2.3",
		// A value that is not an address is not a public host.
		"localhost", "not-an-address", "",
	} {
		if !IsPrivateAddress(address) {
			t.Fatalf("%q reported public", address)
		}
	}
	for _, address := range []string{"1.1.1.1", "8.8.8.8", "93.184.216.34", "172.32.0.1", "192.0.1.1", "2001:4860:4860::8888", "2606:4700::1111", "::ffff:1.1.1.1"} {
		if IsPrivateAddress(address) {
			t.Fatalf("%q reported private", address)
		}
	}
}

// TestIsPrivateDestination resolves the host the way net_guard.js does.
func TestIsPrivateDestination(t *testing.T) {
	public := func(context.Context, string) ([]netip.Addr, error) {
		return []netip.Addr{netip.MustParseAddr("93.184.216.34")}, nil
	}
	internal := func(context.Context, string) ([]netip.Addr, error) {
		return []netip.Addr{netip.MustParseAddr("93.184.216.34"), netip.MustParseAddr("10.0.0.5")}, nil
	}
	failing := func(context.Context, string) ([]netip.Addr, error) {
		return nil, errors.New("no such host")
	}
	ctx := context.Background()
	if IsPrivateDestination(ctx, "https://example.test/v1", public) {
		t.Fatal("a public host was refused")
	}
	if !IsPrivateDestination(ctx, "https://example.test/v1", internal) {
		t.Fatal("a host with one private address was allowed")
	}
	// A host that does not resolve is left to fail as a network error.
	if IsPrivateDestination(ctx, "https://missing.test/v1", failing) {
		t.Fatal("an unresolvable host was called private")
	}
	// An address literal is decided without a lookup.
	lookup := func(context.Context, string) ([]netip.Addr, error) {
		t.Fatal("an address literal was resolved")
		return nil, nil
	}
	if !IsPrivateDestination(ctx, "http://127.0.0.1:11434/v1", lookup) {
		t.Fatal("loopback allowed")
	}
	if IsPrivateDestination(ctx, "http://1.1.1.1/", lookup) {
		t.Fatal("public address refused")
	}
	// A URL that does not parse cannot be vouched for.
	if !IsPrivateDestination(ctx, "://bad", public) {
		t.Fatal("a malformed URL was allowed")
	}
	if !IsPrivateDestination(ctx, "https://[::1]/v1", lookup) {
		t.Fatal("an IPv6 loopback literal was allowed")
	}
}

// TestIsPrivateDestinationUsesTheSystemResolver exercises the default path
// against a name that is not on the network.
func TestIsPrivateDestinationUsesTheSystemResolver(t *testing.T) {
	// A name that cannot resolve is not private, so the call proceeds and fails
	// as a network error; see TestHTTPPublicOnlyRefusesPrivateHosts for the
	// address case.
	if IsPrivateDestination(context.Background(), "https://no-such-host.invalid/v1", nil) {
		t.Fatal("an unresolvable name was called private")
	}
}
