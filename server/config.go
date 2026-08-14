package server

import (
	"fmt"
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
}

// BuildHostPort renders the address the server binds to.
func (c Config) BuildHostPort() string {
	return fmt.Sprintf("%s:%d", c.Host, c.Port)
}
