package dialguard_test

import (
	"net/netip"
	"slices"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ruko1202/xhttp/dialguard"
)

// The addresses below are grouped by what makes them interesting, because the
// grouping is the argument: the first group is what anyone would think to
// block, and the rest is what a string-matching filter misses.
func TestIsInternalBlocksInternalAddresses(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		// One per prefix in the deny list, so a deleted entry fails a named test.
		"this network":       "0.0.0.1",
		"unspecified v4":     "0.0.0.0",
		"RFC1918 10":         "10.0.0.1",
		"RFC1918 172":        "172.16.0.1",
		"RFC1918 192":        "192.168.1.1",
		"CGNAT":              "100.64.0.1",
		"Alibaba metadata":   "100.100.100.200",
		"loopback":           "127.0.0.1",
		"AWS/GCP metadata":   "169.254.169.254",
		"IETF protocol":      "192.0.0.170",
		"TEST-NET-1":         "192.0.2.1",
		"TEST-NET-2":         "198.51.100.1",
		"TEST-NET-3":         "203.0.113.1",
		"6to4 relay anycast": "192.88.99.1",
		"benchmarking":       "198.18.0.1",
		"multicast v4":       "224.0.0.1",
		"reserved":           "240.0.0.1",
		"broadcast":          "255.255.255.255",
		"unspecified v6":     "::",
		"loopback v6":        "::1",
		// ::/96 also spans ::/128 and ::1/128 above, so those two cases would
		// survive deleting their own prefix. TestInternalPrefixesComposition
		// is what pins them.
		"v4-compatible v6":   "::127.0.0.1",
		"v4-translated v6":   "::ffff:0:7f00:1",
		"NAT64":              "64:ff9b::7f00:1",
		"NAT64 local-use":    "64:ff9b:1::a9fe:a9fe",
		"site-local":         "fec0::1",
		"6to4":               "2002:7f00:1::1",
		"unique local":       "fd00::1",
		"link-local v6":      "fe80::1",
		"multicast v6":       "ff02::1",
		"v4-mapped loopback": "::ffff:127.0.0.1",
		"v4-mapped private":  "::ffff:10.0.0.1",
		"v4-mapped metadata": "::ffff:169.254.169.254",
	}

	for name, addr := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assert.True(t, dialguard.IsInternal(netip.MustParseAddr(addr)),
				"%s must be treated as internal", addr)
		})
	}
}

// A zoned link-local address is the case that fails without WithZone(""):
// netip.Prefix.Contains never matches an address carrying a zone.
func TestIsInternalBlocksZonedLinkLocal(t *testing.T) {
	t.Parallel()

	zoned := netip.MustParseAddrPort("[fe80::1%en0]:80").Addr()
	require.NotEmpty(t, zoned.Zone(), "the fixture must actually carry a zone")

	assert.True(t, dialguard.IsInternal(zoned))
}

// The control set. These must stay reachable, or the guard is a blocklist that
// blocks the internet.
func TestIsInternalAllowsPublicAddresses(t *testing.T) {
	t.Parallel()

	for _, addr := range []string{
		"8.8.8.8",
		"1.1.1.1",
		"140.82.121.4",
		"2001:4860:4860::8888",
		"2606:4700::1111",
	} {
		t.Run(addr, func(t *testing.T) {
			t.Parallel()
			assert.False(t, dialguard.IsInternal(netip.MustParseAddr(addr)),
				"%s is public and must not be blocked", addr)
		})
	}
}

func TestInternalPrefixesIsIndependentPerCall(t *testing.T) {
	t.Parallel()

	first, second := dialguard.InternalPrefixes(), dialguard.InternalPrefixes()
	require.Equal(t, first, second)

	// len == cap, so a caller's append allocates rather than writing into a
	// buffer the library also handed to someone else.
	assert.Equal(t, len(first), cap(first))

	first[0] = netip.MustParsePrefix("203.0.113.0/24")
	assert.NotEqual(t, first, dialguard.InternalPrefixes(),
		"mutating the returned slice must not change what the next call returns")
}

func TestBlockingDeniesListedAddress(t *testing.T) {
	t.Parallel()

	guard := dialguard.Blocking(dialguard.InternalPrefixes()...)

	err := guard("tcp4", "169.254.169.254:80")
	require.Error(t, err)
	assert.ErrorIs(t, err, dialguard.ErrBlockedAddress)
	assert.Contains(t, err.Error(), "169.254.169.254",
		"the address is the one fact that makes a blocked dial diagnosable")
}

func TestBlockingPermitsUnlistedAddress(t *testing.T) {
	t.Parallel()

	guard := dialguard.Blocking(dialguard.InternalPrefixes()...)

	assert.NoError(t, guard("tcp4", "8.8.8.8:443"))
}

// Parsing runs before the list is consulted, so an address the guard cannot
// understand is denied even by a guard with an empty deny list.
func TestBlockingFailsClosedOnUnparseableAddress(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct{ network, address string }{
		"unix socket path": {"unix", "/tmp/whatever.sock"},
		"bare hostname":    {"tcp", "example.com:80"},
		"no port":          {"tcp4", "10.0.0.1"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			err := dialguard.Blocking()(tc.network, tc.address)
			require.Error(t, err, "an empty deny list still denies what it cannot parse")
			assert.ErrorIs(t, err, dialguard.ErrBlockedAddress)
			assert.Contains(t, err.Error(), tc.address,
				"the offending string is the only diagnostic available here")
		})
	}
}

func TestBlockingWithNoPrefixesPermitsEveryParseableAddress(t *testing.T) {
	t.Parallel()

	assert.NoError(t, dialguard.Blocking()("tcp4", "127.0.0.1:80"))
}

// The "base list plus or minus my own networks" path — the reason the package
// is exported at all.
func TestBlockingAcceptsACallerAdjustedList(t *testing.T) {
	t.Parallel()

	t.Run("minus a prefix permits that network", func(t *testing.T) {
		t.Parallel()

		rfc1918 := netip.MustParsePrefix("10.0.0.0/8")
		narrowed := slices.DeleteFunc(dialguard.InternalPrefixes(),
			func(p netip.Prefix) bool { return p == rfc1918 })

		guard := dialguard.Blocking(narrowed...)

		assert.NoError(t, guard("tcp4", "10.0.0.1:80"),
			"a caller who removed the prefix must be able to reach it")
		assert.Error(t, guard("tcp4", "127.0.0.1:80"),
			"the rest of the list must keep working")
	})

	t.Run("plus a prefix denies that network", func(t *testing.T) {
		t.Parallel()

		widened := append(dialguard.InternalPrefixes(), netip.MustParsePrefix("198.51.100.0/24"))
		guard := dialguard.Blocking(widened...)

		err := guard("tcp4", "198.51.100.7:80")
		require.Error(t, err)
		assert.ErrorIs(t, err, dialguard.ErrBlockedAddress)
	})
}

// The deny list is the security contract, so its composition is asserted
// directly rather than only through whichever addresses the table above
// samples. Two entries (::/128, ::1/128) are subsumed by ::/96, so removing one
// changes no behavior — this test is the only thing that would notice.
func TestInternalPrefixesComposition(t *testing.T) {
	t.Parallel()

	got := make([]string, 0, len(dialguard.InternalPrefixes()))
	for _, prefix := range dialguard.InternalPrefixes() {
		got = append(got, prefix.String())
	}

	assert.ElementsMatch(t, []string{
		"0.0.0.0/8", "10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16",
		"100.64.0.0/10", "127.0.0.0/8", "169.254.0.0/16", "192.0.0.0/24",
		"192.0.2.0/24", "198.51.100.0/24", "203.0.113.0/24", "192.88.99.0/24",
		"198.18.0.0/15", "224.0.0.0/4", "240.0.0.0/4",
		"::/128", "::1/128", "::/96", "::ffff:0:0:0/96",
		"64:ff9b::/96", "64:ff9b:1::/48", "2002::/16",
		"fc00::/7", "fe80::/10", "fec0::/10", "ff00::/8",
	}, got, "changing the deny list must be deliberate, not incidental")
}

// Blocking copies its prefixes, so a caller who reuses or trims their slice
// cannot loosen a guard already handed out. InternalPrefixes has assertions for
// the same concern; this one had none.
func TestBlockingCopiesItsPrefixes(t *testing.T) {
	t.Parallel()

	prefixes := []netip.Prefix{netip.MustParsePrefix("127.0.0.0/8")}
	guard := dialguard.Blocking(prefixes...)

	prefixes[0] = netip.MustParsePrefix("203.0.113.0/24")

	assert.ErrorIs(t, guard("tcp4", "127.0.0.1:80"), dialguard.ErrBlockedAddress,
		"mutating the caller's slice must not disarm a guard already built")
}

// The guard runs on every dialing goroutine at once, and this code has already
// shipped one data race that a sequential test could not see.
func TestBlockingIsSafeForConcurrentUse(t *testing.T) {
	t.Parallel()

	guard := dialguard.Blocking(dialguard.InternalPrefixes()...)

	const goroutines = 64

	var wg sync.WaitGroup

	errs := make([]error, goroutines)

	for i := range goroutines {
		wg.Add(1)

		go func() {
			defer wg.Done()

			errs[i] = guard("tcp4", "169.254.169.254:80")
		}()
	}

	wg.Wait()

	for i, err := range errs {
		assert.ErrorIs(t, err, dialguard.ErrBlockedAddress, "goroutine %d", i)
	}
}

// A denial refuses one address, not the dial: net.Dialer falls through to the
// next address the resolver returned. So a policy is only effective if it
// covers every address a hostile name could resolve to — in practice, both
// families of each range. This asserts InternalPrefixes is closed that way,
// since a list blocking [::1] but not 127.0.0.1 would block nothing at all.
func TestInternalPrefixesCoverBothFamilies(t *testing.T) {
	t.Parallel()

	pairs := map[string][2]string{
		"loopback":    {"127.0.0.1", "::1"},
		"unspecified": {"0.0.0.0", "::"},
		"link-local":  {"169.254.169.254", "fe80::1"},
		"private":     {"10.0.0.1", "fd00::1"},
		"multicast":   {"224.0.0.1", "ff02::1"},
	}

	for name, pair := range pairs {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			assert.True(t, dialguard.IsInternal(netip.MustParseAddr(pair[0])), "v4 half: %s", pair[0])
			assert.True(t, dialguard.IsInternal(netip.MustParseAddr(pair[1])), "v6 half: %s", pair[1])
		})
	}
}
