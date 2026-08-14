package client_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ruko1202/xhttp/client"
	"github.com/ruko1202/xhttp/sanitize"
)

// TestNoopSanitizerLogsSecrets pins the documented default. It reads like a
// test for a bug, and that is the point: the zero-configuration client logs
// credentials verbatim, so the behavior is asserted rather than left to be
// discovered in production.
func TestNoopSanitizerLogsSecrets(t *testing.T) {
	t.Parallel()

	ctx, dumpLogs := observedContext(t)
	srvURL, _ := echoServer(t)

	httpc := client.NewClient(client.WithCallerBeforeDo(func(_ context.Context, r *http.Request) {
		r.Header.Set("Authorization", "Bearer "+secret)
	}))

	mustDo(ctx, t, httpc, srvURL)

	assert.Contains(t, dumpLogs(), secret,
		"the default sanitizer redacts nothing; if this ever passes, the default changed")
}

// TestWithSanitizerRedactsHeaders covers the seam end to end: a caller-supplied
// policy reaches both the request and the response side.
func TestWithSanitizerRedactsHeaders(t *testing.T) {
	t.Parallel()

	ctx, dumpLogs := observedContext(t)
	srvURL, _ := echoServer(t)

	httpc := client.NewClient(
		client.WithSanitizer(&maskingSanitizer{}),
		client.WithCallerBeforeDo(func(_ context.Context, r *http.Request) {
			r.Header.Set("Authorization", "Bearer "+secret)
		}),
	)

	mustDo(ctx, t, httpc, srvURL)

	logs := dumpLogs()
	assert.NotContains(t, logs, secret, "request Authorization and response Set-Cookie must both be masked")
	assert.Contains(t, logs, redacted)
}

// TestSanitizerSeesHeadersAddedByHooks pins the call order. Hooks run before
// the request is logged, so a credential a hook adds is actually subject to
// redaction. Logging first would make the assertion above self-confirming: it
// would pass with redaction switched off entirely.
func TestSanitizerSeesHeadersAddedByHooks(t *testing.T) {
	t.Parallel()

	ctx, dumpLogs := observedContext(t)
	srvURL, seen := echoServer(t)

	httpc := client.NewClient(
		client.WithSanitizer(&maskingSanitizer{}),
		client.WithCallerBeforeDo(func(_ context.Context, r *http.Request) {
			r.Header.Set("Authorization", "Bearer "+secret)
		}),
	)

	mustDo(ctx, t, httpc, srvURL)

	// The header really was sent — so its absence from the log is redaction,
	// not a hook that failed to run.
	require.Equal(t, "Bearer "+secret, seen.header.Get("Authorization"))
	assert.NotContains(t, dumpLogs(), secret)
}

// TestWithSanitizerNilKeepsPrevious guards the fail-safe direction: a nil
// argument must not install "no redaction".
func TestWithSanitizerNilKeepsPrevious(t *testing.T) {
	t.Parallel()

	ctx, dumpLogs := observedContext(t)
	srvURL, _ := echoServer(t)

	httpc := client.NewClient(
		client.WithSanitizer(&maskingSanitizer{}),
		client.WithSanitizer(nil), // must be ignored, not applied
		client.WithCallerBeforeDo(func(_ context.Context, r *http.Request) {
			r.Header.Set("Authorization", "Bearer "+secret)
		}),
	)

	mustDo(ctx, t, httpc, srvURL)

	assert.NotContains(t, dumpLogs(), secret)
}

// TestSanitizerMasksURL covers the URL path, where a credential is easy to miss
// because it is not a header.
func TestSanitizerMasksURL(t *testing.T) {
	t.Parallel()

	ctx, dumpLogs := observedContext(t)
	srvURL, _ := echoServer(t)

	httpc := client.NewClient(client.WithSanitizer(&maskingSanitizer{}))

	mustDo(ctx, t, httpc, srvURL+"/token/"+secret)

	assert.NotContains(t, dumpLogs(), secret)
}

// TestNoopSanitizerReturnsInputUnchanged is the unit-level statement of the
// default's contract.
func TestNoopSanitizerReturnsInputUnchanged(t *testing.T) {
	t.Parallel()

	s := sanitize.NewNoopSanitizer()
	header := http.Header{"Authorization": []string{"Bearer " + secret}}

	assert.Equal(t, "https://example.com/"+secret, s.SanitizeURL("https://example.com/"+secret))
	assert.Equal(t, header, s.SanitizeHeaders(header))
	assert.Equal(t, []byte(secret), s.SanitizeBody([]byte(secret)))
}
