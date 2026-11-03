package infra_test

import (
	"net/http"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zapcore"

	"github.com/ruko1202/xhttp/infra"
	"github.com/ruko1202/xhttp/lifecycle"
)

func TestReadiness(t *testing.T) {
	t.Parallel()

	t.Run("204 when every check passes", func(t *testing.T) {
		t.Parallel()

		ctx, _ := observedContext(t)
		s := newServer(t, infra.Config{Checks: []infra.Check{okCheck("db"), okCheck("cache")}})

		require.Equal(t, http.StatusNoContent, get(ctx, t, s, "/readiness").Code)
	})

	t.Run("204 with no checks at all", func(t *testing.T) {
		t.Parallel()

		ctx, _ := observedContext(t)
		s := newServer(t, infra.Config{})

		require.Equal(t, http.StatusNoContent, get(ctx, t, s, "/readiness").Code)
	})

	t.Run("500 when a check fails", func(t *testing.T) {
		t.Parallel()

		ctx, _ := observedContext(t)
		s := newServer(t, infra.Config{Checks: []infra.Check{okCheck("cache"), failingCheck("db")}})

		require.Equal(t, http.StatusInternalServerError, get(ctx, t, s, "/readiness").Code)
	})

	t.Run("503 while draining", func(t *testing.T) {
		t.Parallel()

		ctx, _ := observedContext(t)
		s := newServer(t, infra.Config{Drainer: stubDrainer{draining: true}})

		require.Equal(t, http.StatusServiceUnavailable, get(ctx, t, s, "/readiness").Code)
	})

	// The ordering invariant: 503 means "planned drain", 500 means "a
	// dependency is down". Probing first would make a draining replica whose
	// database is already closed report an outage for a routine deploy.
	t.Run("503 wins over a failing check while draining", func(t *testing.T) {
		t.Parallel()

		ctx, _ := observedContext(t)
		s := newServer(t, infra.Config{
			Drainer: stubDrainer{draining: true},
			Checks:  []infra.Check{failingCheck("db")},
		})

		require.Equal(t, http.StatusServiceUnavailable, get(ctx, t, s, "/readiness").Code)
	})

	t.Run("names the failing check in the log", func(t *testing.T) {
		t.Parallel()

		ctx, logs := observedContext(t)
		s := newServer(t, infra.Config{Checks: []infra.Check{failingCheck("postgres")}})

		get(ctx, t, s, "/readiness")

		entries := logs.FilterMessage("readiness check failed").All()
		require.Len(t, entries, 1, "a 500 must say which dependency is down")

		fields := entries[0].ContextMap()
		require.Equal(t, "postgres", fields["check"])
		// The cause, not just the name: knowing "postgres" failed without
		// knowing how leaves the operator exactly where they started.
		require.Contains(t, fields, "error")
		require.Contains(t, entries[0].ContextMap()["error"], errProbe.Error())
	})

	t.Run("panics on a check with no probe", func(t *testing.T) {
		t.Parallel()

		// A wiring typo must not become a replica that reports ready forever
		// without having verified anything.
		require.Panics(t, func() {
			infra.New(infra.Config{Checks: []infra.Check{{Name: "typo"}}})
		})
	})

	t.Run("nil Drainer means never draining", func(t *testing.T) {
		t.Parallel()

		ctx, _ := observedContext(t)
		s := newServer(t, infra.Config{Drainer: nil})

		require.Equal(t, http.StatusNoContent, get(ctx, t, s, "/readiness").Code)
	})

	t.Run("nil Starter means always started", func(t *testing.T) {
		t.Parallel()

		ctx, _ := observedContext(t)
		s := newServer(t, infra.Config{Starter: nil})

		require.Equal(t, http.StatusNoContent, get(ctx, t, s, "/readiness").Code)
	})

	// Start-up is expected, not an incident: the load balancer polls every
	// couple of seconds, and an error line per poll would bury real failures.
	t.Run("503 and no log line while starting", func(t *testing.T) {
		t.Parallel()

		ctx, logs := observedContext(t)
		s := newServer(t, infra.Config{Starter: stubStarter{ready: false}})

		require.Equal(t, http.StatusServiceUnavailable, get(ctx, t, s, "/readiness").Code)
		require.Zero(t, logs.FilterLevelExact(zapcore.ErrorLevel).Len())
	})

	t.Run("204 once started and every check passes", func(t *testing.T) {
		t.Parallel()

		ctx, _ := observedContext(t)
		s := newServer(t, infra.Config{
			Starter: stubStarter{ready: true},
			Checks:  []infra.Check{okCheck("db")},
		})

		require.Equal(t, http.StatusNoContent, get(ctx, t, s, "/readiness").Code)
	})

	t.Run("500 once started and a check fails", func(t *testing.T) {
		t.Parallel()

		ctx, _ := observedContext(t)
		s := newServer(t, infra.Config{
			Starter: stubStarter{ready: true},
			Checks:  []infra.Check{failingCheck("db")},
		})

		require.Equal(t, http.StatusInternalServerError, get(ctx, t, s, "/readiness").Code)
	})

	// A replica told to shut down while still starting reports draining.
	t.Run("503 while draining, even before start-up", func(t *testing.T) {
		t.Parallel()

		ctx, _ := observedContext(t)
		s := newServer(t, infra.Config{
			Drainer: stubDrainer{draining: true},
			Starter: stubStarter{ready: false},
		})

		require.Equal(t, http.StatusServiceUnavailable, get(ctx, t, s, "/readiness").Code)
	})

	// The start-up gate comes before the probes: a dependency that is not yet
	// connected is part of starting, not an outage, so the probe is not run.
	t.Run("503 wins over a failing check while starting", func(t *testing.T) {
		t.Parallel()

		var calls atomic.Int32
		ctx, logs := observedContext(t)
		s := newServer(t, infra.Config{
			Starter: stubStarter{ready: false},
			Checks:  []infra.Check{countingFailingCheck("db", &calls)},
		})

		require.Equal(t, http.StatusServiceUnavailable, get(ctx, t, s, "/readiness").Code)
		require.Zero(t, calls.Load(), "probes must not run before start-up completes")
		require.Zero(t, logs.FilterLevelExact(zapcore.ErrorLevel).Len())
	})

	t.Run("real Starter gates readiness until MarkReady", func(t *testing.T) {
		t.Parallel()

		ctx, _ := observedContext(t)
		starter := lifecycle.NewStarter()
		s := newServer(t, infra.Config{Starter: starter})

		require.Equal(t, http.StatusServiceUnavailable, get(ctx, t, s, "/readiness").Code)

		starter.MarkReady()

		require.Equal(t, http.StatusNoContent, get(ctx, t, s, "/readiness").Code)
	})
}
