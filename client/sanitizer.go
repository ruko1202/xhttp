package client

import "net/http"

// Sanitizer decides what a request or response may look like in a log record.
//
// The transport calls it on every URL, header set and body it is about to log,
// and uses whatever comes back. What counts as a secret is caller knowledge —
// one service treats X-Api-Key as a credential, another uses it as a routing
// hint — so the library provides the seam and the caller provides the policy.
//
// Implementations MUST NOT mutate their argument. The header map and the body
// belong to a live request: http.Client re-enters RoundTrip with the same
// *http.Request on retries and redirects, so a mutating sanitizer corrupts the
// second attempt. Return a copy instead.
//
// Implementations should also be cheap and total. They run on every logged
// exchange, and a panic inside one propagates out of RoundTrip — logging is a
// side effect, and it should never be the reason a request fails.
type Sanitizer interface {
	// SanitizeURL renders a URL for logging. The input is the full URL string,
	// credentials and query included.
	SanitizeURL(u string) string
	// SanitizeHeaders renders a header set for logging.
	SanitizeHeaders(h http.Header) http.Header
	// SanitizeBody renders a body for logging. It is only called when body
	// logging is enabled with WithBodyLogging; the returned bytes are truncated
	// afterwards, so an implementation need not bound its own output.
	SanitizeBody(b []byte) []byte
}

var _ Sanitizer = (*NoopSanitizer)(nil)

// NoopSanitizer returns everything it is given, unchanged.
//
// It is the default, which means a client built without WithSanitizer logs
// URLs, headers and bodies verbatim — Authorization headers, cookies, session
// tokens and any credential carried in a URL path or query included. That is a
// deliberate choice to leave redaction policy entirely to the caller, but it
// makes WithSanitizer the first option to reach for in any service whose
// requests carry credentials.
type NoopSanitizer struct{}

// NewNoopSanitizer returns a Sanitizer that redacts nothing.
func NewNoopSanitizer() *NoopSanitizer {
	return &NoopSanitizer{}
}

// SanitizeURL returns u unchanged.
func (s *NoopSanitizer) SanitizeURL(u string) string {
	return u
}

// SanitizeHeaders returns h unchanged.
func (s *NoopSanitizer) SanitizeHeaders(h http.Header) http.Header {
	return h
}

// SanitizeBody returns b unchanged.
func (s *NoopSanitizer) SanitizeBody(b []byte) []byte {
	return b
}
