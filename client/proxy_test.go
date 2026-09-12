package client_test

import (
	"context"
	"net/http"
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ruko1202/xhttp/client"
)

// A proxy from configuration rather than from the environment, which is what
// this option exists for: net/http reads HTTP_PROXY once per process and caches
// it, so a service that learns its proxy from config has no other way in.
func TestWithProxyRoutesThroughTheGivenProxy(t *testing.T) {
	t.Parallel()

	srvURL, seen := echoServer(t)

	httpc := client.NewClient(client.WithProxy(proxyTo(t, srvURL)))

	// The destination does not resolve; reaching the echo server proves the
	// request went to the proxy instead.
	body := mustDo(context.Background(), t, httpc, "http://unreachable.invalid/")
	assert.Equal(t, `{"ok":true}`, body)
	assert.NotNil(t, seen.header, "the proxy must have received the request")
}

// nil is ignored rather than installed, matching WithSanitizer and
// WithDialGuard — installing it would disable proxying rather than configure
// it. Note the rule points the other way here than for the guard options: what
// survives is ProxyFromEnvironment.
func TestWithProxyNilIsIgnored(t *testing.T) {
	t.Parallel()

	srvURL, _ := echoServer(t)

	httpc := client.NewClient(client.WithProxy(nil))

	assert.Equal(t, `{"ok":true}`, mustDo(context.Background(), t, httpc, srvURL))
}

// Returning (nil, nil) sends the request directly, which is how a caller asks
// for no proxy at all.
func TestWithProxyGoingDirect(t *testing.T) {
	t.Parallel()

	srvURL, _ := echoServer(t)

	httpc := client.NewClient(client.WithProxy(func(*http.Request) (*url.URL, error) {
		return nil, nil
	}))

	assert.Equal(t, `{"ok":true}`, mustDo(context.Background(), t, httpc, srvURL))
}

// WithTransport replaces the transport this option reaches into, so the option
// becomes a silent no-op — the behavior already documented for WithSanitizer
// and the guard options. Pinned so it stays a known property.
func TestWithProxyIsANoopAfterWithTransport(t *testing.T) {
	t.Parallel()

	srvURL, _ := echoServer(t)

	var called bool

	httpc := client.NewClient(
		client.WithTransport(&http.Transport{}),
		client.WithProxy(func(*http.Request) (*url.URL, error) {
			called = true

			return url.Parse(srvURL)
		}),
	)

	assert.Equal(t, `{"ok":true}`, mustDo(context.Background(), t, httpc, srvURL))
	assert.False(t, called, "the option cannot reach a replaced transport")
}

// A guarded client stays guarded when a proxy is configured: the guard still
// refuses the dial it can see. What it cannot see is the destination behind the
// proxy, which is why WithProxy's doc says so.
func TestWithProxyDoesNotDisarmTheDialGuard(t *testing.T) {
	t.Parallel()

	srvURL, _ := echoServer(t)

	httpc := client.NewClient(
		client.WithoutInternalHosts(),
		client.WithProxy(func(*http.Request) (*url.URL, error) { return nil, nil }),
	)

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, srvURL, nil)
	require.NoError(t, err)

	resp, err := httpc.Do(req) //nolint:bodyclose // the dial is refused.
	assert.Nil(t, resp)
	require.Error(t, err, "loopback must still be refused with a proxy configured")
}

// proxyTo routes every request through srvURL, standing in for a configured
// proxy without touching the process environment — net/http caches HTTP_PROXY
// on first use, so t.Setenv cannot be relied on here.
func proxyTo(t *testing.T, srvURL string) func(*http.Request) (*url.URL, error) {
	t.Helper()

	parsed, err := url.Parse(srvURL)
	require.NoError(t, err)

	return func(*http.Request) (*url.URL, error) { return parsed, nil }
}
