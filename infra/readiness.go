package infra

import (
	"net/http"

	"github.com/labstack/echo/v5"
	"github.com/ruko1202/xlog"
	"github.com/ruko1202/xlog/xfield"
)

// readiness reports whether this replica should receive traffic.
//
//	draining          503, no log
//	not yet started   503, no log, probes not run
//	a probe fails     500, error log naming the probe
//	otherwise         204
//
// That order is an invariant rather than an implementation detail. 503 means
// "planned: draining or starting", 500 means "a dependency is down". Running
// the probes first would make a draining replica whose database is already
// closed, or a starting one whose pool is not yet open, report 500 — an outage
// signal, logged on every health-check poll, for what is a routine rolling
// deploy. Drain comes before start-up so a replica told to shut down while
// still starting reports draining.
func (s *Server) readiness(c *echo.Context) error {
	ctx := xlog.WithOperation(c.Request().Context(), "infra.Readiness")

	// During graceful shutdown report not-ready so the load balancer drains
	// this replica before the server stops accepting connections. This is what
	// makes rolling deploys zero-downtime: the proxy ejects us from the pool
	// while we keep serving in-flight requests.
	if s.cfg.Drainer.IsDraining() {
		return c.NoContent(http.StatusServiceUnavailable)
	}

	// Until main declares every module started, hold this replica out of the
	// pool. Not an error, so no log line: the load balancer polls every couple
	// of seconds and start-up is expected to take several polls.
	if !s.cfg.Starter.IsReady() {
		return c.NoContent(http.StatusServiceUnavailable)
	}

	for _, check := range s.cfg.Checks {
		if err := check.Probe(ctx); err != nil {
			xlog.Error(ctx, "readiness check failed",
				xfield.String("check", check.Name),
				xfield.Error(err),
			)

			return c.NoContent(http.StatusInternalServerError)
		}
	}

	return c.NoContent(http.StatusNoContent)
}
