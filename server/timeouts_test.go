package server

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// The tests below live in the internal test package because applyTimeouts is
// unexported. Exercising it directly is deliberate: the alternative is booting a
// real listener and reaching into Echo's http.Server, which tests Echo rather
// than this package.

// TestApplyTimeoutsStampsConfiguredValues is the positive half of the contract:
// every configured timeout reaches the http.Server Echo hands to the hook.
func TestApplyTimeoutsStampsConfiguredValues(t *testing.T) {
	t.Parallel()

	cfg := Config{
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       2 * time.Minute,
	}

	var hs http.Server
	cfg.applyTimeouts(&hs)

	require.Equal(t, 10*time.Second, hs.ReadHeaderTimeout)
	require.Equal(t, 30*time.Second, hs.ReadTimeout)
	require.Equal(t, 60*time.Second, hs.WriteTimeout)
	require.Equal(t, 2*time.Minute, hs.IdleTimeout)
}

// TestApplyTimeoutsLeavesUnsetFieldsAlone is the compatibility half: a Config
// that sets no timeouts must not write anything, so a service upgrading to this
// version keeps the behavior it had.
//
// It asserts against a bare http.Server rather than one Echo built, and that is
// the point of the test rather than a shortcut: Echo pre-sets ReadTimeout to 30s
// on the server it constructs and calls the hook afterwards, so "the field is
// still zero" is only a meaningful statement about THIS function. What is being
// pinned is "applyTimeouts does not touch unset fields", not "the running server
// has no read timeout" — which would be false, and not this package's doing.
func TestApplyTimeoutsLeavesUnsetFieldsAlone(t *testing.T) {
	t.Parallel()

	var hs http.Server
	Config{}.applyTimeouts(&hs)

	require.Zero(t, hs.ReadHeaderTimeout)
	require.Zero(t, hs.ReadTimeout)
	require.Zero(t, hs.WriteTimeout)
	require.Zero(t, hs.IdleTimeout)
}

// TestApplyTimeoutsPreservesExistingValues covers the mixed case, which is the
// one a partial Config actually produces: fields the caller left zero keep
// whatever was already on the server instead of being reset to nothing.
func TestApplyTimeoutsPreservesExistingValues(t *testing.T) {
	t.Parallel()

	// Mirrors what Echo does before calling the hook.
	hs := http.Server{ReadTimeout: 30 * time.Second}

	Config{WriteTimeout: 60 * time.Second}.applyTimeouts(&hs)

	require.Equal(t, 30*time.Second, hs.ReadTimeout, "an unset field must not clear an existing value")
	require.Equal(t, 60*time.Second, hs.WriteTimeout)
}

// TestStartInstallsTimeoutsOnTheRunningServer closes the gap the unit tests
// above leave open: they prove applyTimeouts works, not that Start ever calls
// it. A regression that drops BeforeServeFunc would keep them green while
// shipping a server with no timeouts at all.
//
// It asserts through behavior rather than by reading a field, because the
// http.Server Echo builds is not reachable from here. A connection that opens
// and then sends nothing must be closed by the server once ReadHeaderTimeout
// elapses — which is exactly the slowloris case these timeouts exist for.
func TestStartInstallsTimeoutsOnTheRunningServer(t *testing.T) {
	t.Parallel()

	var lc net.ListenConfig
	ln, err := lc.Listen(context.Background(), "tcp", "127.0.0.1:0")
	require.NoError(t, err)
	port := ln.Addr().(*net.TCPAddr).Port
	require.NoError(t, ln.Close())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	s := New(Config{
		Name: "timeouts",
		Host: "127.0.0.1",
		Port: port,
		// Short enough to keep the test fast, long enough that a loaded CI box
		// still reaches the dial before it fires.
		ReadHeaderTimeout: 300 * time.Millisecond,
	})

	go func() { _ = s.Start(ctx) }()

	conn := dialUntilServing(t, fmt.Sprintf("127.0.0.1:%d", port))
	defer func() { _ = conn.Close() }()

	// Send a request line but never the blank line that ends the headers, then
	// wait for the server to hang up.
	_, err = conn.Write([]byte("GET / HTTP/1.1\r\nHost: localhost\r\n"))
	require.NoError(t, err)

	// The read deadline must be generous relative to ReadHeaderTimeout AND the
	// assertion must distinguish the two ways this read can fail. A bare
	// require.Error passes when the deadline expires, which is precisely the
	// outcome a missing hook produces — the test would then be green for the
	// bug it exists to catch.
	require.NoError(t, conn.SetReadDeadline(time.Now().Add(3*time.Second)))
	_, err = conn.Read(make([]byte, 1))
	require.Error(t, err, "server must close a connection that stalls before the headers end")

	var netErr net.Error
	if errors.As(err, &netErr) {
		require.False(t, netErr.Timeout(),
			"the client's own read deadline expired: the server never closed the connection, "+
				"which means BeforeServeFunc did not install ReadHeaderTimeout")
	}
}

// dialUntilServing waits for the listener to come up, so the test does not race
// the goroutine that starts it.
func dialUntilServing(t *testing.T, addr string) net.Conn {
	t.Helper()

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		dialer := net.Dialer{Timeout: 200 * time.Millisecond}
		conn, err := dialer.DialContext(context.Background(), "tcp", addr)
		if err == nil {
			return conn
		}
		time.Sleep(20 * time.Millisecond)
	}

	t.Fatalf("server never started listening on %s", addr)

	return nil
}
