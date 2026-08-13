package infra_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ruko1202/xhttp/infra"
)

func TestMetrics(t *testing.T) {
	t.Parallel()

	ctx, _ := observedContext(t)
	s := newServer(t, infra.Config{})

	rec := get(ctx, t, s, "/metrics")

	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, rec.Body.String(), "go_goroutines")
}
