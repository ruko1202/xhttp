package server

import (
	"fmt"
	"net/http"
	"time"
)

// DefaultGracefulTimeout bounds how long a shutdown waits for in-flight
// requests before dropping them.
const DefaultGracefulTimeout = 10 * time.Second

// Config is the bind configuration for a Server.
type Config struct {
	// Name identifies the server in log lines. A process typically runs more
	// than one (a public API and an infra port), and the name is what tells
	// their lifecycle messages apart.
	Name string
	Host string
	Port int
	// GracefulTimeout bounds the wait for in-flight requests during shutdown.
	// Zero means DefaultGracefulTimeout.
	//
	// It must exceed the slowest expected in-flight request, and a service
	// draining behind a load balancer should also keep its own drain window
	// longer than the proxy's health-check interval — otherwise the proxy is
	// still routing here when the process tears down.
	GracefulTimeout time.Duration

	// The four http.Server timeouts below are connection hygiene: they are the
	// cheapest defense against slowloris and against connections held open for
	// free. Echo builds the http.Server itself, so these cannot be set from the
	// outside — Start stamps them through StartConfig.BeforeServeFunc.
	//
	// Zero means "leave whatever is already there", NOT "no timeout". That
	// distinction matters because Echo pre-sets ReadTimeout to 30s on the
	// server it builds (its own gosec G112 default) and only then calls the
	// hook; the other three it leaves unset. A library-imposed default here
	// would silently change the behavior of every service that upgrades, so
	// unset fields are left alone.

	// ReadHeaderTimeout caps how long a client may take to send request
	// headers. This is the one that closes slowloris on the header phase.
	ReadHeaderTimeout time.Duration
	// ReadTimeout caps the whole request read, headers and body.
	ReadTimeout time.Duration
	// WriteTimeout caps how long a response may take to write.
	//
	// It must stay above any per-request timeout the service enforces in
	// middleware: a shorter socket deadline severs a slow handler that the
	// application was still willing to run. Leave it zero for SSE or streaming
	// responses, which a fixed write deadline would cut off mid-stream.
	WriteTimeout time.Duration
	// IdleTimeout caps how long an idle keep-alive connection is held open.
	//
	// Keep it above the polling interval of anything that talks to this server
	// on a schedule — health checks, metric scrapes, client heartbeats — or
	// every poll pays for a fresh handshake, which is a cost multiplier rather
	// than a saving.
	IdleTimeout time.Duration
}

// applyTimeouts stamps the configured timeouts onto the http.Server Echo built.
//
// Only non-zero fields are written, so an unset field keeps whatever Echo (or a
// caller's own BeforeServeFunc) already put there.
func (c Config) applyTimeouts(s *http.Server) {
	if c.ReadHeaderTimeout > 0 {
		s.ReadHeaderTimeout = c.ReadHeaderTimeout
	}
	if c.ReadTimeout > 0 {
		s.ReadTimeout = c.ReadTimeout
	}
	if c.WriteTimeout > 0 {
		s.WriteTimeout = c.WriteTimeout
	}
	if c.IdleTimeout > 0 {
		s.IdleTimeout = c.IdleTimeout
	}
}

// BuildHostPort renders the address the server binds to.
func (c Config) BuildHostPort() string {
	return fmt.Sprintf("%s:%d", c.Host, c.Port)
}
