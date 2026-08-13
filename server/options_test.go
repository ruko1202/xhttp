package server_test

import (
	"fmt"
	"io"
	"log/slog"
	"testing"

	"github.com/labstack/echo/v5"
	"github.com/stretchr/testify/require"

	"github.com/ruko1202/xhttp/server"
)

func TestNew(t *testing.T) {
	t.Parallel()

	t.Run("provides an Echo instance", func(t *testing.T) {
		t.Parallel()

		s := server.New(server.Config{Name: "test"})
		require.NotNil(t, s.Echo())
	})

	t.Run("WithEcho is honored by Echo", func(t *testing.T) {
		t.Parallel()

		own := echo.New()
		s := server.New(server.Config{Name: "test"}, server.WithEcho(own))
		require.Same(t, own, s.Echo())
	})

	t.Run("defaults to the package not-found handler", func(t *testing.T) {
		t.Parallel()

		s := server.New(server.Config{Name: "test"})
		require.Error(t, s.NotFoundHandlerFunc()(nil))
	})

	t.Run("WithNotFoundHandler overrides it", func(t *testing.T) {
		t.Parallel()

		sentinel := fmt.Errorf("custom")
		s := server.New(
			server.Config{Name: "test"},
			server.WithNotFoundHandler(func(*echo.Context) error { return sentinel }),
		)
		require.ErrorIs(t, s.NotFoundHandlerFunc()(nil), sentinel)
	})

	t.Run("WithLogger reaches Echo", func(t *testing.T) {
		t.Parallel()

		own := slog.New(slog.NewTextHandler(io.Discard, nil))
		s := server.New(server.Config{Name: "test"}, server.WithLogger(own))
		require.Same(t, own, s.Echo().Logger)
	})
}
