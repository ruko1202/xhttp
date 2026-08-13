package server

import "github.com/labstack/echo/v5"

// NotFoundHandler is the default handler for unmatched routes. It is exported
// because a service that registers its own not-found route still wants the
// same response shape.
func NotFoundHandler(_ *echo.Context) error {
	return echo.ErrNotFound
}

// NotFoundHandlerFunc returns the handler this server uses for unmatched
// routes — NotFoundHandler unless WithNotFoundHandler replaced it. A service
// registering the not-found route itself reads it from here rather than
// hardcoding a choice the option was meant to make.
func (s *Server) NotFoundHandlerFunc() echo.HandlerFunc {
	return s.notFoundHandler
}
