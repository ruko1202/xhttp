package infra

import (
	"net/http"

	"github.com/labstack/echo/v5"
	"github.com/ruko1202/xlog"
	"github.com/ruko1202/xlog/xfield"
)

// readiness reports whether this replica should receive traffic.
//
// Drain is checked BEFORE the probes, and that order is an invariant rather
// than an implementation detail. 503 means "planned drain", 500 means "a
// dependency is down". Running the probes first would make a draining replica
// whose database is already closed report 500 — an outage signal for what is
// a routine rolling deploy.
func (s *Server) readiness(c *echo.Context) error {
	ctx := xlog.WithOperation(c.Request().Context(), "infra.Readiness")

	// During graceful shutdown report not-ready so the load balancer drains
	// this replica before the server stops accepting connections. This is what
	// makes rolling deploys zero-downtime: the proxy ejects us from the pool
	// while we keep serving in-flight requests.
	if s.cfg.Drainer.IsDraining() {
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
