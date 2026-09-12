package client

import (
	"context"
	"net/http"
	"net/url"
	"syscall"
	"time"

	"github.com/ruko1202/xhttp/dialguard"
	"github.com/ruko1202/xhttp/sanitize"
)

// WithTimeout overrides the total request timeout. A non-positive value is
// ignored so a caller can pass an unset config field without accidentally
// disabling the default ceiling.
func WithTimeout(timeout time.Duration) Option {
	return func(c *http.Client) {
		if timeout > 0 {
			c.Timeout = timeout
		}
	}
}

// WithTransport replaces the underlying *http.Transport, keeping the logging
// round-tripper that wraps it. A nil value is ignored.
//
// Note that options which reach into the logging round-tripper —
// WithCallerBeforeDo, WithCallerAfterDo, WithSanitizer, WithBodyLogging,
// WithDialGuard, WithoutInternalHosts, WithProxy — look up that wrapper on the
// client.
// Replacing the whole c.Transport from outside (rather than through this
// option) removes it, and those options then become silent no-ops.
//
// For the two dial-guard options that no-op is a security consequence rather
// than a logging one, and it applies in either order: used before this option
// the guard is installed and then discarded with the transport that carried
// it, and used after, it no longer finds the wrapper. A client built this way
// looks guarded and is not.
func WithTransport(transport *http.Transport) Option {
	return func(c *http.Client) {
		if transport != nil {
			c.Transport = transport
		}
	}
}

// WithDialGuard installs a policy consulted before every connection, refusing
// the dial when it returns an error. It is off by default: this client is also
// used to reach neighbors inside the perimeter and stubs on loopback, and a
// guard enabled by default would break those silently.
//
// Use it when the destination comes from outside the service — a URL an
// administrator types into a form, a callback address in a payload. See
// WithoutInternalHosts for the ready-made policy, and dialguard for building
// one from the default list plus or minus your own networks.
//
// The guard receives an already-resolved ip:port and runs on every connection
// attempt — redirects and retries included, and once per address the resolver
// returned. That is what makes it a defense against DNS rebinding, which a
// check made when a URL is stored cannot be.
//
// Two properties matter when writing a policy of your own. A denial refuses one
// address rather than the dial: net.Dialer falls through to the next address
// the resolver returned, so a list covering one address family and not the
// other blocks nothing. And the guard runs per connection, so a connection it
// approved keeps serving later requests from the pool without being consulted
// again.
//
// A nil guard is ignored, and in particular does not clear a guard an earlier
// option installed: a security control should not disappear because a config
// field was unset. Two non-nil guards do not compose — the last one wins.
func WithDialGuard(guard dialguard.Guard) Option {
	return func(c *http.Client) {
		tr, ok := c.Transport.(*transport)
		if !ok || guard == nil {
			return
		}

		// ControlContext, not Control: net.Dialer documents that a non-nil
		// ControlContext makes Control ignored entirely, and measured on
		// go1.25.13 that is exactly what happens. A guard in the weaker
		// field would be disarmed by any later code setting the stronger
		// one — silently, since the dial would simply succeed and no
		// behavioral test would notice. Installed here it survives the
		// reverse case.
		//
		// Both fields carry a syscall.RawConn the policy has no use for: it
		// is platform-dependent, and an address decision needs only the
		// address. Dropping it keeps that type out of the public API.
		tr.dialer.ControlContext = func(_ context.Context, network, address string, _ syscall.RawConn) error {
			return guard(network, address)
		}
		// Control stays nil: net.Dialer ignores it entirely while
		// ControlContext is set, so leaving it empty keeps the two from
		// ever disagreeing about which hook is live.
		tr.dialer.Control = nil
	}
}

// WithoutInternalHosts refuses connections to loopback, private, link-local and
// reserved addresses — including the IPv6 spellings that carry an IPv4 target
// inside them. It is the ready-made form of WithDialGuard; see dialguard for
// the full list.
//
// Off by default, and worth turning on only where the destination is
// influenced from outside the service. Two limits are worth knowing before
// relying on it:
//
// An HTTP proxy bypasses it. The default transport honors
// http.ProxyFromEnvironment, and with a proxy configured the dialer connects to
// the proxy while the real target travels inside a CONNECT, unseen by the
// guard. NO_PROXY makes this partial rather than all-or-nothing: destinations
// it exempts are dialed directly and are guarded.
//
// WithTransport removes it, in either option order. Replacing the transport
// discards the dialer this guard lives on, and an option applied afterwards no
// longer finds the wrapper it needs — so the client looks configured and is
// not.
//
// The error names an address, not a host: the guard sees only what the
// resolver returned, so a message telling an operator which URL was rejected
// has to be produced by the caller.
func WithoutInternalHosts() Option {
	return WithDialGuard(dialguard.Blocking(dialguard.InternalPrefixes()...))
}

// WithProxy overrides how the client picks a proxy for a request, replacing the
// default http.ProxyFromEnvironment. Returning a nil *url.URL for a request
// sends it directly.
//
// A nil function is ignored rather than installed, since that would disable
// proxying rather than configure it — pass a function returning (nil, nil) if
// going direct is what you want.
//
// Note the same rule points the other way here than it does for WithDialGuard.
// There, ignoring nil preserves a security control; here it preserves
// http.ProxyFromEnvironment, which is the thing that defeats a dial guard. "nil
// is ignored" means "the earlier setting stands", not "fails safe".
//
// Worth knowing when a dial guard is installed: a proxied request is not
// covered by it. The dialer connects to the proxy, so that is the only address
// the guard ever sees; the real destination is carried in the request itself —
// measured on go1.25.13, in the request line for http and in a CONNECT for
// https. Routing a guarded client through a proxy therefore disables the
// protection for every proxied destination, whatever the scheme.
//
// Do not interpolate the proxy URL into an error this function returns. Such an
// error is surfaced by net/http and logged at error level without passing
// through the sanitizer, and a proxy URL routinely carries credentials — that
// is how HTTPS_PROXY auth is expressed. Name the host, not the URL.
func WithProxy(proxy func(*http.Request) (*url.URL, error)) Option {
	return func(c *http.Client) {
		tr, ok := c.Transport.(*transport)
		if !ok || proxy == nil {
			return
		}

		tr.tr.Proxy = proxy
	}
}

// WithoutRedirect stops the client from following redirects, returning the
// redirect response itself instead.
func WithoutRedirect() Option {
	return WithCustomRedirectFlow(func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	})
}

// WithCustomRedirectFlow installs a custom http.Client.CheckRedirect policy.
func WithCustomRedirectFlow(redirectFlow func(*http.Request, []*http.Request) error) Option {
	return func(c *http.Client) {
		c.CheckRedirect = redirectFlow
	}
}

// WithSanitizer installs the redaction policy used for every logged URL, header
// set and body. Without it the client uses sanitize.NoopSanitizer and logs
// everything verbatim, secrets included.
//
// A nil sanitizer is ignored rather than installed: a client that logs raw
// credentials because a constructor argument was nil is worse than one that
// keeps whatever policy it already had.
func WithSanitizer(s sanitize.Sanitizer) Option {
	return func(c *http.Client) {
		tr, ok := c.Transport.(*transport)
		if !ok || s == nil {
			return
		}

		tr.sanitizer = s
	}
}

// WithCallerBeforeDo registers a hook run just before the request goes out. Use
// it to attach credentials or correlation headers:
//
//	WithCallerBeforeDo(func(_ context.Context, r *http.Request) {
//	    r.Header.Set("Authorization", "Bearer "+token)
//	})
//
// Hooks run before the request is logged, so anything they add is visible to
// the sanitizer — and therefore actually subject to redaction.
func WithCallerBeforeDo(f func(context.Context, *http.Request)) Option {
	return func(c *http.Client) {
		tr, ok := c.Transport.(*transport)
		if !ok {
			return
		}

		tr.beforeRoundTrip = append(tr.beforeRoundTrip, f)
	}
}

// WithCallerAfterDo registers a hook run after a successful round-trip. It is
// not called when the round-trip fails.
//
// A hook that reads resp.Body must restore it, or the caller will read an empty
// body.
func WithCallerAfterDo(f func(context.Context, *http.Response)) Option {
	return func(c *http.Client) {
		tr, ok := c.Transport.(*transport)
		if !ok {
			return
		}

		//nolint:bodyclose // stores the hook for later; it neither owns nor reads the body here.
		tr.afterRoundTrip = append(tr.afterRoundTrip, f)
	}
}

// WithBodyLogging enables request and response body dumps at debug level,
// truncated to maxLoggedBodyBytes.
//
// This is a local debugging tool and is deliberately awkward to reach for.
// Bodies are the one thing a sanitizer struggles to make safe: their shape is
// arbitrary, so a secret inside one is unrecognizable. Enabling this in code
// that ships means every payload the service exchanges lands in the log.
//
// Consider forbidding it outside tests with a lint rule, e.g. golangci-lint's
// forbidigo.
func WithBodyLogging() Option {
	return func(c *http.Client) {
		tr, ok := c.Transport.(*transport)
		if !ok {
			return
		}

		tr.logBodies = true
	}
}
