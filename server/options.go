package server

import (
	"log/slog"

	"github.com/labstack/echo/v5"
)

// Option configures a Server.
type Option func(*Server)

// WithEcho replaces the Echo instance the server wraps. Use it when the
// service needs to configure Echo itself (a custom Binder, Validator or
// HTTPErrorHandler) before any route is registered.
func WithEcho(e *echo.Echo) Option {
	return func(s *Server) {
		s.e = e
	}
}

// WithLogger sets the logger Echo uses.
//
// Echo v5 logs through *slog.Logger. A service logging through something else
// passes an adapter — the lifecycle lines this package writes itself go
// through xlog and are unaffected by this option.
func WithLogger(l *slog.Logger) Option {
	return func(s *Server) {
		s.e.Logger = l
	}
}

// WithNotFoundHandler overrides the handler used for unmatched routes.
func WithNotFoundHandler(h echo.HandlerFunc) Option {
	return func(s *Server) {
		s.notFoundHandler = h
	}
}
