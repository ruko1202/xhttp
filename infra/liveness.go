package infra

import (
	"net/http"

	"github.com/labstack/echo/v5"
)

// liveness reports that the process is up. It deliberately checks nothing: a
// liveness probe that consults dependencies turns a database blip into a
// restart loop, which is the opposite of what it is for.
func (s *Server) liveness(c *echo.Context) error {
	return c.NoContent(http.StatusNoContent)
}
