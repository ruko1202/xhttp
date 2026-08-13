package infra_test

import (
	"net/http"
	"slices"
	"testing"

	"github.com/labstack/echo/v5"
	"github.com/ruko1202/swaggerui"
	"github.com/stretchr/testify/require"

	"github.com/ruko1202/xhttp/infra"
	"github.com/ruko1202/xhttp/server"
)

// alwaysOnRoutes must be served in every environment, sorted as routes()
// returns them. Alerts, dashboards and proxy health checks are pinned to these
// paths, so the tests assert the exact set rather than spot-checking one.
//
// The catch-all is registered as a not-found route rather than a GET, which is
// why it sorts under Echo's own method name.
var alwaysOnRoutes = []string{
	"GET /liveness",
	"GET /metrics",
	"GET /readiness",
	"GET /version",
	"echo_route_not_found /*",
}

func TestRouteTable(t *testing.T) {
	t.Parallel()

	// Exact set, not a subset: a route accidentally added outside dev widens
	// the surface of a port that is not meant to be published, and a subset
	// assertion would wave it through.
	t.Run("serves exactly the operational routes outside dev", func(t *testing.T) {
		t.Parallel()

		s := newServer(t, infra.Config{Dev: false})

		require.Equal(t, alwaysOnRoutes, nonPprofRoutes(t, s))
	})

	t.Run("serves exactly the operational routes plus the dev ones in dev", func(t *testing.T) {
		t.Parallel()

		s := newServer(t, infra.Config{
			Dev:             true,
			Specs:           []swaggerui.Spec{{Name: "api", Content: []byte("{}")}},
			PrimarySpecName: "api",
		})

		want := slices.Concat(alwaysOnRoutes, []string{"GET /", "GET /swagger/*"})
		slices.Sort(want)

		require.Equal(t, want, nonPprofRoutes(t, s))
	})

	t.Run("serves every scraped pprof profile in every environment", func(t *testing.T) {
		t.Parallel()

		// Not gated behind Dev on purpose: a profile scraper reaches these in
		// production, which is exactly where continuous profiling is worth
		// having. Checked by name because a scraper asks for specific
		// profiles — asserting only the index would stay green if the rest
		// stopped being registered.
		for _, dev := range []bool{true, false} {
			s := newServer(t, infra.Config{Dev: dev})
			registered := routes(t, s)

			for _, profile := range []string{
				"GET /debug/pprof/",
				"GET /debug/pprof/profile",
				"GET /debug/pprof/heap",
				"GET /debug/pprof/goroutine",
				"GET /debug/pprof/block",
				"GET /debug/pprof/mutex",
			} {
				require.Contains(t, registered, profile, "dev=%v", dev)
			}
		}
	})

	t.Run("hides the index and swagger outside dev", func(t *testing.T) {
		t.Parallel()

		s := newServer(t, infra.Config{
			Dev:             false,
			Specs:           []swaggerui.Spec{{Name: "api", Content: []byte("{}")}},
			PrimarySpecName: "api",
		})

		registered := routes(t, s)
		require.NotContains(t, registered, "GET /")
		require.NotContains(t, registered, "GET /swagger/*")
	})

	t.Run("serves the index in dev", func(t *testing.T) {
		t.Parallel()

		s := newServer(t, infra.Config{Dev: true})

		require.Contains(t, routes(t, s), "GET /")
	})

	t.Run("serves swagger in dev when specs are supplied", func(t *testing.T) {
		t.Parallel()

		s := newServer(t, infra.Config{
			Dev:             true,
			Specs:           []swaggerui.Spec{{Name: "api", Content: []byte("{}")}},
			PrimarySpecName: "api",
		})

		require.Contains(t, routes(t, s), "GET /swagger/*")
	})

	t.Run("omits swagger in dev without specs", func(t *testing.T) {
		t.Parallel()

		s := newServer(t, infra.Config{Dev: true})

		require.NotContains(t, routes(t, s), "GET /swagger/*")
	})
}

func TestServerOptionsPassThrough(t *testing.T) {
	t.Parallel()

	t.Run("WithNotFoundHandler reaches the not-found route", func(t *testing.T) {
		t.Parallel()

		ctx, _ := observedContext(t)
		s := infra.New(
			infra.Config{Version: testVersion},
			server.WithNotFoundHandler(func(c *echo.Context) error {
				return c.String(http.StatusTeapot, "gone fishing")
			}),
		)

		rec := get(ctx, t, s, "/nope")

		require.Equal(t, http.StatusTeapot, rec.Code)
	})
}

// TestHealthProbesAreNotLogged pins the middleware asymmetry: health checks
// poll every couple of seconds forever, so logging them would bury every other
// line. A 404 is logged, because someone asking for a route that does not
// exist is worth a line.
func TestHealthProbesAreNotLogged(t *testing.T) {
	t.Parallel()

	t.Run("liveness and readiness stay silent", func(t *testing.T) {
		t.Parallel()

		ctx, logs := observedContext(t)
		s := newServer(t, infra.Config{})

		get(ctx, t, s, "/liveness")
		get(ctx, t, s, "/readiness")

		require.Empty(t, logs.FilterMessage("REQUEST").All())
	})

	t.Run("a 404 is logged", func(t *testing.T) {
		t.Parallel()

		ctx, logs := observedContext(t)
		s := newServer(t, infra.Config{})

		rec := get(ctx, t, s, "/nope")

		require.Equal(t, http.StatusNotFound, rec.Code)
		require.NotEmpty(t, logs.FilterMessageSnippet("REQUEST").All())
	})
}
