// Package infra provides a complete, self-starting infrastructure server: the
// endpoints an operator, a load balancer and a monitoring stack expect on a
// service's internal port.
//
// Unlike server, this package is not a shell — it registers its own routes and
// owns them. That is the point: the paths below are what alerts, dashboards
// and proxy health checks are pinned to, so every service must expose the same
// ones. Handing out handlers instead would let each service wire its own
// paths, and they would drift.
//
// Always registered:
//
//	GET /liveness         204, unconditionally — the process is running
//	GET /readiness        204 ready, 503 draining, 500 a dependency is down
//	GET /version          200, the build metadata supplied in Config
//	GET /metrics          200, the Prometheus default registry
//	GET /debug/pprof/*    the standard profiling endpoints
//
// Registered only when Config.Dev is set:
//
//	GET /                 an index of the endpoints above
//	GET /swagger/*        the Swagger UI, when Config.Specs is non-empty
//
// # Exposure
//
// None of these endpoints is authenticated, and /debug/pprof in particular
// serves heap dumps — memory contents, which can include tokens and other
// secrets held in buffers. This server is safe to run only on a port that is
// not published past the network perimeter. Binding it to a publicly reachable
// address hands an attacker profiling data and metrics for free; that is the
// caller's decision to get right, not something this package can enforce.
package infra

import (
	"context"

	"github.com/ruko1202/swaggerui"

	"github.com/ruko1202/xhttp/lifecycle"
	"github.com/ruko1202/xhttp/server"
)

// Config describes the infra server.
type Config struct {
	// Server is the bind configuration passed through to the HTTP shell.
	Server server.Config

	// Dev gates the endpoints meant for humans rather than machines: the index
	// page and the Swagger UI.
	Dev bool

	// Version is served verbatim by /version.
	Version VersionInfo

	// Checks are the readiness probes. All must pass for /readiness to report
	// ready; probes run in order and the first failure decides the response.
	// A Check with a nil Probe makes New panic.
	Checks []Check

	// Drainer reports whether the process has begun graceful shutdown. A nil
	// value means "never draining", which is the right default for a service
	// that does not implement draining — not a reason to panic on every probe.
	Drainer lifecycle.DrainChecker

	// Specs are the OpenAPI documents offered in the Swagger UI selector. Empty
	// means no /swagger route, even in dev.
	Specs []swaggerui.Spec

	// PrimarySpecName selects which of Specs the UI opens by default.
	PrimarySpecName string
}

// Check is a readiness probe. Name identifies it in the log line written when
// the probe fails — with more than one dependency, an anonymous 500 does not
// tell an operator which one is down.
type Check struct {
	Name  string
	Probe func(ctx context.Context) error
}

// VersionInfo is the build metadata served by /version.
//
// The json tags are a wire contract: consumers parse these names, so they are
// part of the API rather than a serialization detail.
type VersionInfo struct {
	AppName   string `json:"app_name"`
	Version   string `json:"version"`
	OS        string `json:"os"`
	Arch      string `json:"arch"`
	BuildTime string `json:"build_time"`
	ShaCommit string `json:"sha_commit"`
}
