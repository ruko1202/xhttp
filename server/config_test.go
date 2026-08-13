package server_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ruko1202/xhttp/server"
)

func TestConfig(t *testing.T) {
	t.Parallel()

	t.Run("BuildHostPort joins host and port", func(t *testing.T) {
		t.Parallel()

		cfg := server.Config{Host: "127.0.0.1", Port: 8080}
		require.Equal(t, "127.0.0.1:8080", cfg.BuildHostPort())
	})
}

// TestDefaultGracefulTimeout pins the documented default. Services size their
// own drain window against this number — MaintMode's is deliberately shorter —
// so changing it silently changes the shutdown budget of every consumer.
//
// This asserts the constant, not the drain: measuring the real timeout means
// holding a request open for the full ten seconds, which buys a slow, timing-
// dependent test for a value that is passed straight to Echo.
func TestDefaultGracefulTimeout(t *testing.T) {
	t.Parallel()

	require.Equal(t, 10*time.Second, server.DefaultGracefulTimeout)
}
