// Package server provides the Echo v5 lifecycle shell a service wraps its
// HTTP surface in: construction, start, graceful stop, and the generic
// request-logging middlewares.
//
// The package is deliberately thin. It owns no routes and no middleware chain
// — those are the service's business. What it owns is the part every service
// would otherwise reimplement: binding a configured address, starting Echo in
// a way that unblocks on context cancellation, and shutting it down.
//
// The dependency on Echo v5 is hard and intentional: there is no abstraction
// over the router. Reach the underlying instance with Echo() to register
// routes, middlewares, a Binder, a Validator or an error handler; pass an
// already-configured instance in with WithEcho.
package server

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync/atomic"

	"github.com/labstack/echo/v5"
	"github.com/ruko1202/xlog"
)

// Server is an Echo instance plus its lifecycle. Construct it with New.
type Server struct {
	cfg Config
	e   *echo.Echo
	// serverStopF is written by Start and read by Stop, which run on different
	// goroutines by construction: a service starts the server in one and stops
	// it from its shutdown path in another. A plain field here is a data race
	// that -race reports and that can, in principle, lose the cancel func and
	// hang a shutdown.
	serverStopF     atomic.Pointer[context.CancelFunc]
	notFoundHandler echo.HandlerFunc
}

// New creates a Server bound to cfg.
func New(cfg Config, opts ...Option) *Server {
	s := &Server{
		cfg:             cfg,
		e:               echo.New(),
		notFoundHandler: NotFoundHandler,
	}

	// Stop before Start is a no-op rather than a nil dereference: a service that
	// fails during construction and tears down what it has built should not
	// panic on the way out.
	noop := context.CancelFunc(func() {})
	s.serverStopF.Store(&noop)

	for _, opt := range opts {
		opt(s)
	}

	return s
}

// Echo returns the underlying Echo instance, so the service can register
// routes and middlewares.
//
// It is meant for configuration between New and Start. Mutating the router
// after Start is not supported.
func (s *Server) Echo() *echo.Echo {
	return s.e
}

// Start begins listening and blocks until the server stops.
// Returns an error only if startup fails, otherwise blocks until shutdown.
// The provided context is used to stop blocking when canceled.
// Actual shutdown is performed by the Stop method.
func (s *Server) Start(ctx context.Context) error {
	xlog.Infof(ctx, "starting http server '%s' on %s", s.cfg.Name, s.cfg.BuildHostPort())

	ctx, cancel := context.WithCancel(ctx)
	s.serverStopF.Store(&cancel)

	eCfg := echo.StartConfig{
		Address:         s.cfg.BuildHostPort(),
		HideBanner:      true,
		GracefulTimeout: cmp.Or(s.cfg.GracefulTimeout, DefaultGracefulTimeout),
		// Echo builds the http.Server internally, so this hook — which runs
		// after the listener binds and before Serve — is the only place its
		// timeouts can be set. It is installed unconditionally and stamps only
		// non-zero fields, so an all-zero Config leaves the server exactly as
		// Echo built it.
		BeforeServeFunc: func(hs *http.Server) error {
			s.cfg.applyTimeouts(hs)

			return nil
		},
		OnShutdownError: func(err error) {
			xlog.Errorf(ctx, "shutdown http server '%s' failed: %v", s.cfg.Name, err)
		},
	}

	// Start server in a goroutine to allow context-based unblocking.
	errCh := make(chan error, 1)
	go func() {
		errCh <- eCfg.Start(ctx, s.e)
	}()

	// Wait for either server startup error or context cancellation.
	select {
	case err := <-errCh:
		cancel()
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("failed to start http server '%s': %w", s.cfg.Name, err)
		}

		return nil
	case <-ctx.Done():
		cancel()
		// Context canceled, unblock the Start call.
		// Actual shutdown will be performed by Stop method via closer.
		xlog.Infof(ctx, "http server '%s' start unblocked due to context cancellation", s.cfg.Name)

		return nil
	}
}

// Stop signals the server to shut down.
//
// It does NOT wait for in-flight requests: it cancels the context Start is
// blocked on and returns immediately, leaving Echo to drain in the goroutine
// Start launched. A caller that must not tear down its dependencies while
// requests are still running has to hold its own drain window — closing a
// database right after Stop returns can fail a request that is still being
// served.
func (s *Server) Stop(ctx context.Context) error {
	xlog.Infof(ctx, "shutdown http server: '%s'", s.cfg.Name)

	(*s.serverStopF.Load())()

	xlog.Infof(ctx, "http server is shutdown: '%s'", s.cfg.Name)

	return nil
}
