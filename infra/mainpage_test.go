package infra_test

import (
	"net/http"
	"testing"

	"github.com/ruko1202/swaggerui"
	"github.com/stretchr/testify/require"

	"github.com/ruko1202/xhttp/infra"
)

func TestMainPage(t *testing.T) {
	t.Parallel()

	t.Run("names the service and renders as html", func(t *testing.T) {
		t.Parallel()

		ctx, _ := observedContext(t)
		s := newServer(t, infra.Config{Dev: true, Version: testVersion})

		rec := get(ctx, t, s, "/")

		require.Equal(t, http.StatusOK, rec.Code)
		require.Contains(t, rec.Body.String(), testVersion.AppName)
		require.Contains(t, rec.Header().Get("Content-Type"), "text/html")
	})

	// The swagger link is conditional on specs being supplied, so both sides
	// of that branch need covering — otherwise a broken condition shows up as
	// a missing link in someone's browser rather than a failing test.
	t.Run("links to swagger only when specs are supplied", func(t *testing.T) {
		t.Parallel()

		ctx, _ := observedContext(t)

		withSpecs := newServer(t, infra.Config{
			Dev:             true,
			Specs:           []swaggerui.Spec{{Name: "api", Content: []byte("{}")}},
			PrimarySpecName: "api",
		})
		require.Contains(t, get(ctx, t, withSpecs, "/").Body.String(), "swagger")

		withoutSpecs := newServer(t, infra.Config{Dev: true})
		require.NotContains(t, get(ctx, t, withoutSpecs, "/").Body.String(), "swagger")
	})
}
