package server_test

import (
	"testing"

	"github.com/labstack/echo/v5"
	"github.com/stretchr/testify/require"

	"github.com/ruko1202/xhttp/server"
)

func TestNotFoundHandler(t *testing.T) {
	t.Parallel()

	require.ErrorIs(t, server.NotFoundHandler(nil), echo.ErrNotFound)
}
