package infra_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ruko1202/xhttp/infra"
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
}
