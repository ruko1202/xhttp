package infra_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ruko1202/xhttp/infra"
)

func TestLiveness(t *testing.T) {
	t.Parallel()

	ctx, _ := observedContext(t)
	s := newServer(t, infra.Config{})

	// Deliberately checks nothing: a liveness probe consulting dependencies
	// turns a database blip into a restart loop.
	rec := get(ctx, t, s, "/liveness")

	require.Equal(t, http.StatusNoContent, rec.Code)
}
