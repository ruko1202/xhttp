package client_test

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ruko1202/xhttp/client"
)

func TestNewClientDefaults(t *testing.T) {
	t.Parallel()

	httpc := client.NewClient()

	assert.Equal(t, 30*time.Second, httpc.Timeout)
	assert.NotNil(t, httpc.Transport, "the logging round-tripper must be installed")
	assert.Nil(t, httpc.CheckRedirect, "redirects follow the stdlib default unless configured")
}

func TestClientLogsExchange(t *testing.T) {
	t.Parallel()

	ctx, dumpLogs := observedContext(t)
	srvURL, _ := echoServer(t)

	body := mustDo(ctx, t, client.NewClient(), srvURL)
	require.Equal(t, `{"ok":true}`, body, "logging must not consume the response body")

	logs := dumpLogs()
	assert.Contains(t, logs, "sending request")
	assert.Contains(t, logs, "received response")
	assert.Contains(t, logs, "200 OK")
}

// A failed round-trip must log an error and surface it, not swallow it.
func TestClientLogsTransportError(t *testing.T) {
	t.Parallel()

	ctx, dumpLogs := observedContext(t)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, deadServerURL(t), nil)
	require.NoError(t, err)

	resp, err := client.NewClient().Do(req) //nolint:bodyclose // the request fails; there is no body to close.
	require.Error(t, err)
	require.Nil(t, resp)

	assert.Contains(t, dumpLogs(), "failed to execute request")
}

func TestWithTimeout(t *testing.T) {
	t.Parallel()

	t.Run("positive value overrides the default", func(t *testing.T) {
		t.Parallel()

		httpc := client.NewClient(client.WithTimeout(2 * time.Second))
		assert.Equal(t, 2*time.Second, httpc.Timeout)
	})

	// A caller passing an unset config field must not end up with no ceiling at
	// all, which is why non-positive values are ignored rather than applied.
	t.Run("non-positive value is ignored", func(t *testing.T) {
		t.Parallel()

		assert.Equal(t, 30*time.Second, client.NewClient(client.WithTimeout(0)).Timeout)
		assert.Equal(t, 30*time.Second, client.NewClient(client.WithTimeout(-time.Second)).Timeout)
	})
}

func TestRedirectOptions(t *testing.T) {
	t.Parallel()

	srvURL := redirectServer(t)

	t.Run("followed by default", func(t *testing.T) {
		t.Parallel()

		ctx, _ := observedContext(t)
		assert.Equal(t, "arrived", mustDo(ctx, t, client.NewClient(), srvURL+"/start"))
	})

	t.Run("WithoutRedirect returns the redirect itself", func(t *testing.T) {
		t.Parallel()

		ctx, _ := observedContext(t)

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, srvURL+"/start", nil)
		require.NoError(t, err)

		resp, err := client.NewClient(client.WithoutRedirect()).Do(req)
		require.NoError(t, err)
		defer func() { _ = resp.Body.Close() }()

		assert.Equal(t, http.StatusFound, resp.StatusCode)
	})
}

func TestCallerHooks(t *testing.T) {
	t.Parallel()

	ctx, _ := observedContext(t)
	srvURL, seen := echoServer(t)

	var afterStatus string

	httpc := client.NewClient(
		client.WithCallerBeforeDo(func(_ context.Context, r *http.Request) {
			r.Header.Set("X-Trace", "hook")
		}),
		client.WithCallerAfterDo(func(_ context.Context, resp *http.Response) {
			afterStatus = resp.Status
		}),
	)

	mustDo(ctx, t, httpc, srvURL)

	assert.Equal(t, "hook", seen.header.Get("X-Trace"), "the before hook must reach the wire")
	assert.Equal(t, "200 OK", afterStatus, "the after hook must see the response")
}

// Options that configure the logging round-tripper look it up by type. When a
// caller swaps the whole Transport out from under them they must degrade to
// no-ops rather than panic.
func TestOptionsAreNoopsOnForeignTransport(t *testing.T) {
	t.Parallel()

	httpc := client.NewClient()
	httpc.Transport = http.DefaultTransport

	assert.NotPanics(t, func() {
		client.WithBodyLogging()(httpc)
		client.WithSanitizer(&maskingSanitizer{})(httpc)
		client.WithCallerBeforeDo(func(context.Context, *http.Request) {})(httpc)
		client.WithCallerAfterDo(func(context.Context, *http.Response) {})(httpc)
	})
}

func TestWithTransport(t *testing.T) {
	t.Parallel()

	custom := &http.Transport{MaxIdleConns: 7}

	assert.Same(t, custom, client.NewClient(client.WithTransport(custom)).Transport)

	// nil must not blank the transport the client was built with.
	assert.NotNil(t, client.NewClient(client.WithTransport(nil)).Transport)
}

func TestPublicSurfaceIsUsableFromOutside(t *testing.T) {
	t.Parallel()

	// Compiling this file at all proves the exported API is self-sufficient:
	// the package is client_test, so an unexported type leaking into a public
	// signature would fail to build here.
	var _ client.Sanitizer = client.NewNoopSanitizer()

	opts := []client.Option{
		client.WithTimeout(time.Second),
		client.WithSanitizer(client.NewNoopSanitizer()),
		client.WithoutRedirect(),
		client.WithCustomRedirectFlow(func(*http.Request, []*http.Request) error { return nil }),
	}

	require.NotNil(t, client.NewClient(opts...))
}
