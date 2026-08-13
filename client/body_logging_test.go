package client_test

import (
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ruko1202/xhttp/client"
)

// Bodies are off unless asked for, and the placeholder keeps "not logged"
// distinguishable from "empty".
func TestBodiesAreNotLoggedByDefault(t *testing.T) {
	t.Parallel()

	ctx, dumpLogs := observedContext(t)
	srvURL, _ := echoServer(t)

	// A distinct needle: the response Set-Cookie also carries `secret`, and the
	// default sanitizer does not redact it, so asserting on `secret` here would
	// fail for a reason that has nothing to do with bodies.
	const bodyNeedle = "BODY-ONLY-NEEDLE"

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, srvURL, strings.NewReader(bodyNeedle))
	require.NoError(t, err)

	resp, err := client.NewClient().Do(req)
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())

	logs := dumpLogs()
	assert.NotContains(t, logs, bodyNeedle)
	assert.Contains(t, logs, "body logging disabled")
}

func TestWithBodyLoggingLogsBodies(t *testing.T) {
	t.Parallel()

	ctx, dumpLogs := observedContext(t)
	srvURL, _ := echoServer(t)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, srvURL, strings.NewReader("hello"))
	require.NoError(t, err)

	resp, err := client.NewClient(client.WithBodyLogging()).Do(req)
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())

	logs := dumpLogs()
	assert.Contains(t, logs, "hello", "request body")
	assert.Contains(t, logs, `{\"ok\":true}`, "response body")
}

// Dumping reads the body to log it, so it must put it back. Otherwise enabling
// a logging option would silently send an empty request and hand the caller an
// empty response — a debug switch turning into data loss.
func TestBodyLoggingPreservesBodies(t *testing.T) {
	t.Parallel()

	ctx, _ := observedContext(t)
	srvURL, seen := echoServer(t)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, srvURL, strings.NewReader("payload"))
	require.NoError(t, err)

	resp, err := client.NewClient(client.WithBodyLogging()).Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	got, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	assert.Equal(t, "payload", string(seen.body), "the whole request body must reach the wire")
	assert.Equal(t, `{"ok":true}`, string(got), "the caller must still read the whole response")
}

// Truncation bounds the log line, not the wire.
func TestBodyLoggingTruncatesLongBodies(t *testing.T) {
	t.Parallel()

	ctx, dumpLogs := observedContext(t)
	srvURL, seen := echoServer(t)

	const bodySize = 5 << 10 // 5 KiB, past the 4 KiB cap
	long := strings.Repeat("a", bodySize)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, srvURL, strings.NewReader(long))
	require.NoError(t, err)

	resp, err := client.NewClient(client.WithBodyLogging()).Do(req)
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())

	assert.Len(t, seen.body, bodySize, "the full body still goes out")
	assert.Contains(t, dumpLogs(), "[TRUNCATED]", "the logged copy is cut and marked")
}

// The sanitizer must see the body before it is truncated: policy first, length
// second. The other order would hand the sanitizer a pre-cut payload, hiding
// anything past the cap from redaction.
func TestSanitizerRunsBeforeTruncation(t *testing.T) {
	t.Parallel()

	ctx, dumpLogs := observedContext(t)
	srvURL, _ := echoServer(t)

	const bodySize = 5 << 10
	sanitizer := &maskingSanitizer{}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, srvURL, strings.NewReader(strings.Repeat("b", bodySize)))
	require.NoError(t, err)

	httpc := client.NewClient(client.WithBodyLogging(), client.WithSanitizer(sanitizer))

	resp, err := httpc.Do(req)
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())

	assert.Len(t, sanitizer.seenBody(), bodySize, "the sanitizer sees the whole body, not a truncated one")
	assert.Contains(t, dumpLogs(), redacted)
}
