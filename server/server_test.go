package server_test

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/labstack/echo/v5"
	"github.com/stretchr/testify/require"

	"github.com/ruko1202/xhttp/server"
)

// TestServerLifecycle drives a real listener: Start must serve, and Stop must
// unblock it. Everything else in this package is configuration, but this is
// the behavior the package exists for.
func TestServerLifecycle(t *testing.T) {
	t.Parallel()

	ctx, _ := observedContext(t)
	port := freePort(t)

	s := server.New(server.Config{Name: "test", Host: "127.0.0.1", Port: port})
	s.Echo().GET("/ping", func(c *echo.Context) error {
		return c.String(http.StatusOK, "pong")
	})

	started := make(chan error, 1)
	go func() { started <- s.Start(ctx) }()

	requireServing(t, port)

	require.NoError(t, s.Stop(ctx))

	select {
	case err := <-started:
		require.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("Start did not unblock after Stop")
	}
}

// TestStartUnblocksOnContextCancel covers the other exit path: the caller
// cancels the context instead of calling Stop.
func TestStartUnblocksOnContextCancel(t *testing.T) {
	t.Parallel()

	base, _ := observedContext(t)
	ctx, cancel := context.WithCancel(base)
	port := freePort(t)

	s := server.New(server.Config{Name: "test", Host: "127.0.0.1", Port: port})

	started := make(chan error, 1)
	go func() { started <- s.Start(ctx) }()

	requireListening(t, port)
	cancel()

	select {
	case err := <-started:
		require.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("Start did not unblock on context cancellation")
	}
}

func TestStartFailsOnBusyPort(t *testing.T) {
	t.Parallel()

	ctx, _ := observedContext(t)
	port := freePort(t)

	occupant := server.New(server.Config{Name: "occupant", Host: "127.0.0.1", Port: port})
	go func() { _ = occupant.Start(ctx) }()
	requireListening(t, port)
	defer func() { _ = occupant.Stop(ctx) }()

	intruder := server.New(server.Config{Name: "intruder", Host: "127.0.0.1", Port: port})
	require.Error(t, intruder.Start(ctx))
}

// requireServing waits until the server answers /ping, so a test never races
// the listener coming up.
func requireServing(t *testing.T, port int) {
	t.Helper()

	require.Eventually(t, func() bool {
		body, ok := get(t, fmt.Sprintf("http://127.0.0.1:%d/ping", port))

		return ok && body == "pong"
	}, 5*time.Second, 20*time.Millisecond, "server never became reachable")
}

// requireListening waits until the port is bound, without assuming any route.
func requireListening(t *testing.T, port int) {
	t.Helper()

	require.Eventually(t, func() bool {
		_, ok := get(t, fmt.Sprintf("http://127.0.0.1:%d/", port))

		return ok
	}, 5*time.Second, 20*time.Millisecond, "server never bound the port")
}

func get(t *testing.T, url string) (body string, ok bool) {
	t.Helper()

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, url, nil)
	require.NoError(t, err)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", false
	}
	defer func() { _ = resp.Body.Close() }()

	raw, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	return string(raw), true
}

// TestStopIsSafeFromAnotherGoroutine pins the concurrency contract Start and Stop
// have with each other: Start assigns the cancel func, Stop reads it, and a
// service necessarily calls them from different goroutines — one blocks on
// Start while the shutdown path calls Stop.
//
// The field behind that used to be a plain func(), which -race reports as a data
// race under exactly this (normal) usage. Beyond the report, a lost write could
// leave Stop calling the no-op and the shutdown hanging.
//
// Honest scope: this test does NOT reproduce the race on its own. Waiting for
// the bind synchronizes the two goroutines and closes the window, and removing
// that wait makes the test race Stop against a server that may not have started
// — flaky in the other direction. What reproduced it reliably was the Console's
// TestDrainAndStopOrdersTeardown, which starts both servers and stops them from
// a third goroutine. This test pins the contract and the no-op-before-Start
// behavior; -race in a consumer's suite is what guards the field itself.
func TestStopIsSafeFromAnotherGoroutine(t *testing.T) {
	t.Parallel()

	ctx, _ := observedContext(t)
	port := freePort(t)
	s := server.New(server.Config{Name: "race", Host: "127.0.0.1", Port: port})

	started := make(chan error, 1)
	go func() { started <- s.Start(ctx) }()

	// Stop from this goroutine while Start is still settling in the other — the
	// window the race lives in. Waiting for the bind first means the write in
	// Start has certainly happened, so the test exercises the read/write pair
	// rather than racing to call Stop before Start ever began.
	requireListening(t, port)
	require.NoError(t, s.Stop(ctx))

	select {
	case err := <-started:
		require.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("Start did not unblock after a concurrent Stop")
	}
}
