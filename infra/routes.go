package infra

import (
	"net/http"

	"github.com/labstack/echo-contrib/v5/pprof"
	"github.com/labstack/echo/v5"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/ruko1202/swaggerui"

	"github.com/ruko1202/xhttp/server"
)

func (s *Server) bindRouters() {
	e := s.Echo()

	e.Use(server.BaseMiddlewares()...)

	// Registered in EVERY environment, unlike the dev-only routes below — this
	// is deliberate, not an oversight. A profile scraper (Alloy, Pyroscope's
	// agent, or a human with `go tool pprof`) reaches these on the infra port,
	// so gating them behind Dev would blind continuous profiling exactly where
	// it earns its keep: in production.
	//
	// This is safe only because the infra port is not meant to be published
	// past the perimeter — see the package doc. Registered directly on the
	// Echo instance rather than on the group below, so its ordering against
	// the catch-all not-found route is unchanged.
	pprof.Register(e, pprof.DefaultPrefix)

	gr := e.Group("")

	// Request logging is attached ONLY to the not-found route, not to the
	// group. Health checks poll /liveness and /readiness every couple of
	// seconds forever; logging them would bury every other line in the file.
	// A 404 here, by contrast, means someone is asking for something that does
	// not exist, which is worth a line.
	gr.RouteNotFound("/*", s.NotFoundHandlerFunc(), server.RequestLoggingMiddleware())

	gr.Add(http.MethodGet, "/liveness", s.liveness)
	gr.Add(http.MethodGet, "/readiness", s.readiness)
	gr.Add(http.MethodGet, "/version", s.version)
	gr.Add(http.MethodGet, "/metrics", echo.WrapHandler(promhttp.Handler()))

	if !s.cfg.Dev {
		return
	}

	gr.Add(http.MethodGet, "/", s.mainPage)

	if len(s.cfg.Specs) > 0 {
		// The handler has its own internal mux rooted at "/", so it is mounted
		// under /swagger with the prefix stripped; open it at /swagger/ — the
		// trailing slash matters, because the UI assets use relative paths.
		gr.Add(http.MethodGet, "/swagger/*", echo.WrapHandler(http.StripPrefix(
			"/swagger",
			swaggerui.HandlerWithSpecs(s.cfg.Specs, swaggerui.WithPrimaryName(s.cfg.PrimarySpecName)),
		)))
	}
}
