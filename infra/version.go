package infra

import (
	"net/http"

	"github.com/labstack/echo/v5"
)

// version serves the build metadata supplied in Config.
func (s *Server) version(c *echo.Context) error {
	return c.JSON(http.StatusOK, s.cfg.Version)
}
