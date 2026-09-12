// Package dialguard holds the address policy a dialer consults before it
// connects.
//
// It stands on its own so a service can name a network policy without
// importing the client, and so the deny list is testable on addresses rather
// than through a socket.
//
// The policy belongs at the dialer rather than at the URL for two reasons.
// Measured on go1.25.13, the spellings 127.1, 2130706433, 0x7f000001 and
// 017700000001 are all rejected by net.ParseIP and netip.ParseAddr, and all
// resolved to 127.0.0.1 by the resolver — "failed to parse" is not "will not be
// reached", and any check on the URL string lives in that gap. More
// importantly, a name may resolve differently when it is fetched than when it
// was stored, so only a check made at connect time closes DNS rebinding.
package dialguard

import (
	"errors"
	"fmt"
	"net/netip"
	"slices"
)

// ErrBlockedAddress is wrapped by every denial, so a caller can tell a blocked
// dial from a network failure:
//
//	if errors.Is(err, dialguard.ErrBlockedAddress) { ... }
//
// It survives the wrapping http.Client applies on the way out — *net.OpError
// and then *url.Error — and so is still recognizable on the error returned from
// Do.
var ErrBlockedAddress = errors.New("blocked address")

// Guard reports whether a connection to address may proceed, returning a
// non-nil error to refuse it.
//
// It is deliberately narrower than net.Dialer.Control, whose third parameter is
// a syscall.RawConn: that type is platform-dependent and no address policy
// needs it. The client adapts this shape to Control's.
//
// address arrives already resolved, in ip:port form. A Guard runs on every
// connection attempt — including redirects, retries, and once per address the
// resolver returned — and must be safe for concurrent use.
//
// A denial rejects one address, not the whole dial. Measured on go1.25.13: when
// a name resolves to several addresses, net.Dialer treats a refusal as a failed
// attempt and moves on to the next, so denying [::1] still connects to
// 127.0.0.1. A policy must therefore be closed over every address a hostile
// name might return — denying one family while permitting the other blocks
// nothing. InternalPrefixes covers both families of every range it lists for
// this reason; a hand-built list that does not is bypassable.
//
// It also runs per connection, not per request. A connection the policy
// approved stays in the client's pool and serves later requests without
// consulting the guard again.
type Guard func(network, address string) error

// internalPrefixes is the deny list. It is materialized as explicit prefixes
// rather than assembled from the net.IP predicates, because those cover less
// than they appear to: IsPrivate is only RFC1918 and fc00::/7, and
// IsUnspecified matches 0.0.0.0 alone rather than the 0.0.0.0/8 around it.
// Holding one model instead of two — a prefix list plus a predicate set that
// must be kept in step — is what keeps IsInternal and Blocking from disagreeing.
var internalPrefixes = []netip.Prefix{
	// "This network". 0.0.0.1 routes to loopback on Linux and is a classic
	// filter bypass.
	netip.MustParsePrefix("0.0.0.0/8"),
	netip.MustParsePrefix("10.0.0.0/8"),
	netip.MustParsePrefix("172.16.0.0/12"),
	netip.MustParsePrefix("192.168.0.0/16"),
	// CGNAT. Several clouds put internal networks here; 100.100.100.200 is
	// Alibaba's metadata endpoint.
	netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("127.0.0.0/8"),
	// Link-local — where the AWS and GCP metadata endpoint lives, and the
	// reason this package exists.
	netip.MustParsePrefix("169.254.0.0/16"),
	// IETF protocol assignments: DS-Lite 192.0.0.0/29 and the NAT64 well-known
	// addresses. Distinct from TEST-NET-1 below.
	netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("198.51.100.0/24"),
	netip.MustParsePrefix("203.0.113.0/24"),
	// 6to4 anycast relay.
	netip.MustParsePrefix("192.88.99.0/24"),
	netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("224.0.0.0/4"),
	// Reserved; also covers 255.255.255.255.
	netip.MustParsePrefix("240.0.0.0/4"),
	netip.MustParsePrefix("::/128"),
	netip.MustParsePrefix("::1/128"),
	// The four entries below carry an IPv4 target inside an IPv6 address, and
	// none of them satisfies a single stdlib predicate — which is why guarding
	// by predicate leaves all four open.
	//
	// IPv4-compatible IPv6 (deprecated, still parsed).
	netip.MustParsePrefix("::/96"),
	// IPv4-translated (RFC 2765). Not to be confused with IPv4-mapped
	// (::ffff:127.0.0.1): translated addresses report Is4In6 as false, Unmap is
	// a no-op on them, and ::/96 does not contain them, so this entry is the
	// only thing standing between ::ffff:0:7f00:1 and loopback.
	netip.MustParsePrefix("::ffff:0:0:0/96"),
	netip.MustParsePrefix("64:ff9b::/96"),
	// NAT64 local-use (RFC 8215). The well-known /96 above is not enough:
	// this is the range an operator is meant to pick for their own
	// translator, so 64:ff9b:1::a9fe:a9fe reaches cloud metadata wherever
	// one is deployed.
	netip.MustParsePrefix("64:ff9b:1::/48"),
	// 6to4: 2002:7f00:1::1 wraps 127.0.0.1.
	netip.MustParsePrefix("2002::/16"),
	netip.MustParsePrefix("fc00::/7"),
	netip.MustParsePrefix("fe80::/10"),
	// Site-local, deprecated by RFC 3879 but still parsed and still routed
	// — and fe80::/10 stops at febf::, so without this fec0::1 is
	// reachable. The list already carries deprecated ranges (::/96,
	// 192.88.99.0/24) for the same reason: what a stack still routes is
	// what an attacker can still reach.
	netip.MustParsePrefix("fec0::/10"),
	netip.MustParsePrefix("ff00::/8"),
}

// InternalPrefixes returns the default deny list: loopback, private, link-local
// and reserved ranges, plus the IPv6 forms that carry an IPv4 target inside
// them.
//
// It is a function rather than a variable, and returns a fresh slice with
// len == cap, so a caller can build their own policy from it —
// append(InternalPrefixes(), mine) — without writing into a buffer the library
// also handed to someone else.
func InternalPrefixes() []netip.Prefix {
	out := make([]netip.Prefix, len(internalPrefixes))
	copy(out, internalPrefixes)

	return out
}

// IsInternal reports whether addr belongs to any range in InternalPrefixes.
//
// It canonicalizes addr first, so a caller holding an address from any source
// — building an allow-list, validating a form — gets the same answer the guard
// would give. Both steps are load-bearing: netip.Prefix.Contains matches on
// address family, so 127.0.0.0/8 does not contain ::ffff:127.0.0.1 until it is
// unmapped, and never contains a zoned address such as fe80::1%en0 until the
// zone is dropped.
func IsInternal(addr netip.Addr) bool {
	return containsAddr(internalPrefixes, addr)
}

// Blocking returns a Guard denying any address inside prefixes.
//
// The prefixes are copied at construction, so a later change to the caller's
// slice cannot loosen a guard already in use.
//
// An address that does not parse is denied whatever the list contains, since
// the parse happens first: a guard that fails open on input it does not
// understand is one an attacker only has to confuse. Note this also denies
// unix-socket dials, whose address is a filesystem path — the intended
// outcome for a guard installed against an attacker-influenced destination.
//
// Called with no prefixes it denies nothing that parses, which is what an empty
// deny list means.
func Blocking(prefixes ...netip.Prefix) Guard {
	owned := make([]netip.Prefix, len(prefixes))
	copy(owned, prefixes)

	return func(_, address string) error {
		addrPort, err := netip.ParseAddrPort(address)
		if err != nil {
			return fmt.Errorf("%w: %q is not an ip:port address: %w", ErrBlockedAddress, address, err)
		}

		if containsAddr(owned, addrPort.Addr()) {
			return fmt.Errorf("%w: %s", ErrBlockedAddress, address)
		}

		return nil
	}
}

// containsAddr is the single place canonicalization happens, so the exported
// predicate and the guard cannot drift apart.
func containsAddr(prefixes []netip.Prefix, addr netip.Addr) bool {
	canonical := addr.Unmap().WithZone("")

	return slices.ContainsFunc(prefixes, func(prefix netip.Prefix) bool {
		return prefix.Contains(canonical)
	})
}
