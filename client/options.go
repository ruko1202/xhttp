package client

import (
	"context"
	"net/http"
	"time"

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
// WithCallerBeforeDo, WithCallerAfterDo, WithSanitizer, WithBodyLogging — look
// up that wrapper on the client. Replacing the whole c.Transport from outside
// (rather than through this option) removes it, and those options then become
// silent no-ops.
func WithTransport(transport *http.Transport) Option {
	return func(c *http.Client) {
		if transport != nil {
			c.Transport = transport
		}
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
