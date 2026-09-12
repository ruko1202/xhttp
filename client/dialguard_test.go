package client_test

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ruko1202/xhttp/client"
	"github.com/ruko1202/xhttp/dialguard"
)

// Mutation record. These tests were checked against the implementation with
// each load-bearing line removed, so they are known to fail when the guard
// stops working rather than merely passing alongside it. Observed on
// go1.25.13/darwin:
//
//	remove tr.dialer.Control (client/options.go)
//	    red: TestWithDialGuardRefusesTheDial, TestWithDialGuardPermitsWhenGuardReturnsNil,
//	         TestWithDialGuardNilIsIgnored, TestWithDialGuardLastNonNilWins,
//	         TestWithDialGuardAcceptsADialguardGuard, TestWithoutInternalHostsBlocksLoopback,
//	         TestWithoutInternalHostsBlocksObscureLoopbackSpellings,
//	         TestWithoutInternalHostsBlocksMappedLiterals,
//	         TestWithoutInternalHostsBlocksMetadataAndPrivateAddresses,
//	         TestWithoutInternalHostsBlocksTLS, TestDialGuardRefusesARedirectHop
//
//	remove Unmap() from containsAddr (dialguard/dialguard.go)
//	    red: TestIsInternalBlocksInternalAddresses at v4-mapped_loopback,
//	         v4-mapped_private and v4-mapped_metadata
//
//	remove WithZone("") from containsAddr (dialguard/dialguard.go)
//	    red: TestIsInternalBlocksZonedLinkLocal
//
// Note the second and third mutations are invisible from here: the dialer
// unmaps IPv4-mapped literals itself before Control runs, so the client-level
// tests pass without Unmap. The unit tests in dialguard are what pin it.

// A guard that refuses everything must actually stop the dial — the whole
// point of the option is that it reaches the dialer the transport uses.
func TestWithDialGuardRefusesTheDial(t *testing.T) {
	t.Parallel()

	srvURL, _ := echoServer(t)

	httpc := client.NewClient(client.WithDialGuard(func(_, _ string) error {
		return assert.AnError
	}))

	resp, err := doGet(t, httpc, srvURL) //nolint:bodyclose // the dial fails; there is no body.
	require.Error(t, err)
	assert.Nil(t, resp)
	assert.ErrorIs(t, err, assert.AnError, "the guard's own error must reach the caller")
}

func TestWithDialGuardPermitsWhenGuardReturnsNil(t *testing.T) {
	t.Parallel()

	srvURL, _ := echoServer(t)

	var calledWith []string

	httpc := client.NewClient(client.WithDialGuard(func(network, address string) error {
		calledWith = append(calledWith, network+" "+address)
		return nil
	}))

	body := mustDo(context.Background(), t, httpc, srvURL)
	assert.Equal(t, `{"ok":true}`, body)
	assert.NotEmpty(t, calledWith, "the guard must be consulted on the way out")
}

// nil is ignored rather than installed, matching WithSanitizer and
// WithTransport — and, more importantly, it must not disarm a guard an earlier
// option already installed. Silently removing a security control because a
// config field was unset is the failure worth preventing here.
func TestWithDialGuardNilIsIgnored(t *testing.T) {
	t.Parallel()

	t.Run("on its own it is a no-op", func(t *testing.T) {
		t.Parallel()

		srvURL, _ := echoServer(t)
		httpc := client.NewClient(client.WithDialGuard(nil))

		assert.Equal(t, `{"ok":true}`, mustDo(context.Background(), t, httpc, srvURL))
	})

	t.Run("it does not clear an installed guard", func(t *testing.T) {
		t.Parallel()

		srvURL, _ := echoServer(t)
		httpc := client.NewClient(
			client.WithDialGuard(func(_, _ string) error { return assert.AnError }),
			client.WithDialGuard(nil),
		)

		resp, err := doGet(t, httpc, srvURL) //nolint:bodyclose // the dial fails; there is no body.
		require.Error(t, err)
		assert.Nil(t, resp)
		assert.ErrorIs(t, err, assert.AnError)
	})
}

// A dialguard.Guard must drop straight into the option: the two-argument shape
// exists so a policy never has to name syscall.RawConn.
func TestWithDialGuardAcceptsADialguardGuard(t *testing.T) {
	t.Parallel()

	srvURL, _ := echoServer(t)

	httpc := client.NewClient(client.WithDialGuard(dialguard.Blocking(dialguard.InternalPrefixes()...)))

	resp, err := doGet(t, httpc, srvURL) //nolint:bodyclose // the dial fails; there is no body.
	require.Error(t, err)
	assert.Nil(t, resp)
	assert.ErrorIs(t, err, dialguard.ErrBlockedAddress)
}

// doGet issues a GET the way the tests above need it: with a context, and
// returning the error rather than failing the test, because the error is what
// they assert on.
func doGet(t *testing.T, httpc *http.Client, url string) (*http.Response, error) {
	t.Helper()

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, url, nil)
	require.NoError(t, err)

	return httpc.Do(req) //nolint:bodyclose // callers own the response.
}

// The default must keep dialing anywhere: xhttp is also used to reach a
// neighbor on a private address or a stub on loopback, and a guard on by
// default would break those silently.
func TestNewClientDialsLoopbackByDefault(t *testing.T) {
	t.Parallel()

	srvURL, _ := echoServer(t)

	assert.Equal(t, `{"ok":true}`, mustDo(context.Background(), t, client.NewClient(), srvURL))
}

// The sentinel has to survive the wrapping http.Client applies on the way out
// — *net.OpError, then *url.Error — or a caller cannot tell a blocked dial
// from a timeout without parsing strings.
func TestWithoutInternalHostsBlocksLoopback(t *testing.T) {
	t.Parallel()

	srvURL, _ := echoServer(t)

	httpc := client.NewClient(client.WithoutInternalHosts())

	resp, err := doGet(t, httpc, srvURL) //nolint:bodyclose // the dial fails; there is no body.
	require.Error(t, err)
	assert.Nil(t, resp)
	assert.ErrorIs(t, err, dialguard.ErrBlockedAddress,
		"the sentinel must survive *net.OpError and *url.Error")
}

// Spellings both stdlib parsers reject and the resolver expands to 127.0.0.1.
// Whether they resolve is a property of the host's resolver rather than of this
// library, so the assertion is that the request did not succeed and names which
// layer refused it — a future red here should not be read as a guard
// regression without checking that first.
func TestWithoutInternalHostsBlocksObscureLoopbackSpellings(t *testing.T) {
	t.Parallel()

	httpc := client.NewClient(client.WithoutInternalHosts())

	for _, host := range []string{"127.1", "2130706433"} {
		t.Run(host, func(t *testing.T) {
			t.Parallel()

			resp, err := doGet(t, httpc, "http://"+host+":80/") //nolint:bodyclose // the dial fails.
			require.Error(t, err)
			assert.Nil(t, resp)

			var dnsErr *net.DNSError
			assert.True(t,
				errors.Is(err, dialguard.ErrBlockedAddress) || errors.As(err, &dnsErr),
				"expected the guard or a resolution failure, got %v", err)
		})
	}
}

// assertGuardBlocksHosts runs one subtest per host, asserting a guarded client
// refuses to dial it. Each host is written straight into the URL, so the
// refusal can only come from the guard.
func assertGuardBlocksHosts(t *testing.T, hosts ...string) {
	t.Helper()

	httpc := client.NewClient(client.WithoutInternalHosts())

	for _, host := range hosts {
		t.Run(host, func(t *testing.T) {
			t.Parallel()

			resp, err := doGet(t, httpc, "http://"+host+":80/") //nolint:bodyclose // the dial fails.
			require.Error(t, err)
			assert.Nil(t, resp)
			assert.ErrorIs(t, err, dialguard.ErrBlockedAddress)
		})
	}
}

// IPv4-mapped literals reach the guard already unmapped — the dialer does that
// itself — so these prove the end-to-end path, not that canonicalization works.
// TestIsInternalBlocks* in dialguard is what pins Unmap.
func TestWithoutInternalHostsBlocksMappedLiterals(t *testing.T) {
	t.Parallel()

	assertGuardBlocksHosts(t, "[::ffff:127.0.0.1]", "[::ffff:10.0.0.1]")
}

// The addresses the change exists for: cloud metadata, a private neighbor, and
// an IPv6 unique-local target.
func TestWithoutInternalHostsBlocksMetadataAndPrivateAddresses(t *testing.T) {
	t.Parallel()

	assertGuardBlocksHosts(t, "169.254.169.254", "10.0.0.1", "[fc00::1]")
}

// HTTPS must be guarded too — the motivating case is a discovery URL over TLS,
// and a guard that only covered plaintext would miss it entirely.
//
// The control matters as much as the assertion. httptest serves a certificate
// nothing trusts, so a guarded and an unguarded client both fail against it and
// an assertion on "the request failed" would stay green with the guard deleted.
// The control here therefore trusts the certificate and *succeeds*, which is
// what makes the guarded client's refusal evidence: the dial was reachable, and
// only the guard stopped it.
//
// The control has to reach TLS trust through WithTransport, which is exactly
// why it cannot be the guarded client — replacing the transport discards the
// guard (see limitation 2 on WithoutInternalHosts). So the two clients differ
// in two ways rather than one; the comparison holds because the control proves
// the dial itself was permitted.
func TestWithoutInternalHostsBlocksTLS(t *testing.T) {
	t.Parallel()

	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("arrived"))
	}))
	t.Cleanup(srv.Close)

	pool := x509.NewCertPool()
	pool.AddCert(srv.Certificate())

	control := client.NewClient(client.WithTransport(&http.Transport{
		TLSClientConfig: &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12},
	}))
	require.Equal(t, "arrived", mustDo(context.Background(), t, control, srv.URL),
		"the control must reach the server, or a refusal below proves nothing")

	guarded := client.NewClient(client.WithoutInternalHosts())

	resp, err := doGet(t, guarded, srv.URL) //nolint:bodyclose // the dial fails.
	require.Error(t, err)
	assert.Nil(t, resp)
	assert.ErrorIs(t, err, dialguard.ErrBlockedAddress,
		"an HTTPS dial must reach the guard, not slip past it via DialTLSContext")

	// And the refusal came before the handshake, not from it: a certificate
	// error here would mean the dial was permitted after all.
	var certErr *tls.CertificateVerificationError
	assert.NotErrorAs(t, err, &certErr)
}

// A redirect to an internal address is the case the whole design rests on:
// Control runs per connection, so a destination that only becomes internal
// after the first hop is still refused. Every other case here writes the
// internal address straight into the URL, which is the easy half.
func TestDialGuardRefusesARedirectHop(t *testing.T) {
	t.Parallel()

	internal, _ := echoServer(t)

	redirector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, internal, http.StatusFound)
	}))
	t.Cleanup(redirector.Close)

	internalAddr := strings.TrimPrefix(internal, "http://")

	// Permit the first hop and refuse the second, so the refusal can only come
	// from the redirected connection.
	httpc := client.NewClient(client.WithDialGuard(func(_, address string) error {
		if address == internalAddr {
			return fmt.Errorf("%w: %s", dialguard.ErrBlockedAddress, address)
		}

		return nil
	}))

	resp, err := doGet(t, httpc, redirector.URL) //nolint:bodyclose // the redirected dial fails.
	require.Error(t, err)
	assert.Nil(t, resp)
	assert.ErrorIs(t, err, dialguard.ErrBlockedAddress,
		"the guard must run again on the redirected connection")
}

// Pinned as a known property, not an aspiration: replacing the transport drops
// the guard whichever order the options are given in.
func TestWithTransportDiscardsTheGuardInEitherOrder(t *testing.T) {
	t.Parallel()

	// Each subtest gets its own server: echoServer records the request it saw
	// into a shared struct, so pointing two parallel subtests at one instance
	// races on that recording.
	t.Run("transport first", func(t *testing.T) {
		t.Parallel()

		srvURL, _ := echoServer(t)
		httpc := client.NewClient(
			client.WithTransport(&http.Transport{}),
			client.WithoutInternalHosts(),
		)

		assert.Equal(t, `{"ok":true}`, mustDo(context.Background(), t, httpc, srvURL))
	})

	t.Run("guard first", func(t *testing.T) {
		t.Parallel()

		srvURL, _ := echoServer(t)
		httpc := client.NewClient(
			client.WithoutInternalHosts(),
			client.WithTransport(&http.Transport{}),
		)

		assert.Equal(t, `{"ok":true}`, mustDo(context.Background(), t, httpc, srvURL))
	})
}

// Two guards do not compose: the later non-nil one replaces the earlier
// outright, rather than both running.
//
// Which dialer field the guard occupies cannot be observed from here — both
// net.Dialer fields are unexported through this API, and nothing outside the
// package can install a competing hook. That choice (ControlContext, because a
// non-nil ControlContext makes net.Dialer ignore Control entirely) is pinned by
// the comment at its site in options.go and by TestDialerFieldPrecedence in
// package client, not by this test.
func TestLaterGuardReplacesEarlier(t *testing.T) {
	t.Parallel()

	srvURL, _ := echoServer(t)

	httpc := client.NewClient(client.WithoutInternalHosts())
	client.WithDialGuard(func(_, _ string) error { return nil })(httpc)

	assert.Equal(t, `{"ok":true}`, mustDo(context.Background(), t, httpc, srvURL),
		"the last non-nil guard wins outright")
}
