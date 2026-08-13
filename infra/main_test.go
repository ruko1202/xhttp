// This file holds the fixtures shared by the tests in this package. The
// *_test.go files next to it contain assertions only.
package infra_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"

	"github.com/ruko1202/xlog"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"

	"github.com/ruko1202/xhttp/infra"
)

// testVersion is the build metadata every test serves, with values distinct
// enough that a mixed-up field is visible in a failure message.
var testVersion = infra.VersionInfo{
	AppName:   "test-app",
	Version:   "v1.2.3",
	OS:        "linux",
	Arch:      "amd64",
	BuildTime: "2026-01-01T00:00:00Z",
	ShaCommit: "abc1234",
}

// errProbe is what a failing readiness check returns.
var errProbe = errors.New("dependency down")

// stubDrainer reports a fixed drain state.
type stubDrainer struct{ draining bool }

func (d stubDrainer) IsDraining() bool { return d.draining }

// observedContext returns a context carrying a logger whose records are
// captured, so tests can assert on what an operator would see.
func observedContext(t *testing.T) (ctx context.Context, logs *observer.ObservedLogs) {
	t.Helper()

	core, recorded := observer.New(zapcore.DebugLevel)
	ctx = xlog.ContextWithLogger(context.Background(), xlog.NewZapAdapter(zap.New(core)))

	return ctx, recorded
}

// get issues a request against the server's Echo instance without binding a
// port, and returns the recorder.
func get(ctx context.Context, t *testing.T, s *infra.Server, target string) *httptest.ResponseRecorder {
	t.Helper()

	req := httptest.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	rec := httptest.NewRecorder()
	s.Echo().ServeHTTP(rec, req)

	return rec
}

// nonPprofRoutes returns the registered routes with the pprof ones filtered
// out, so a test can assert an exact set. pprof registers a dozen paths of its
// own; those are covered by name in their own test.
func nonPprofRoutes(t *testing.T, s *infra.Server) []string {
	t.Helper()

	var out []string
	for _, r := range routes(t, s) {
		if strings.Contains(r, "/debug/pprof") {
			continue
		}

		out = append(out, r)
	}

	return out
}

// routes returns the registered "METHOD PATH" pairs, sorted.
func routes(t *testing.T, s *infra.Server) []string {
	t.Helper()

	registered := s.Echo().Router().Routes()

	out := make([]string, 0, len(registered))
	for _, r := range registered {
		out = append(out, r.Method+" "+r.Path)
	}
	sort.Strings(out)

	return out
}

// okCheck is a readiness probe that always passes.
func okCheck(name string) infra.Check {
	return infra.Check{Name: name, Probe: func(context.Context) error { return nil }}
}

// failingCheck is a readiness probe that always fails.
func failingCheck(name string) infra.Check {
	return infra.Check{Name: name, Probe: func(context.Context) error { return errProbe }}
}

// newServer builds an infra server for the given config, defaulting the parts
// a test does not care about.
func newServer(t *testing.T, cfg infra.Config) *infra.Server {
	t.Helper()

	if cfg.Version == (infra.VersionInfo{}) {
		cfg.Version = testVersion
	}

	return infra.New(cfg)
}
