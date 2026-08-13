// Package xhttp provides HTTP building blocks for Go services, with structured
// logging and tracing wired in.
//
// The packages live below this one — see xhttp/client for the HTTP client. This
// root package only exposes the tracing setup shared by them.
package xhttp

import (
	"go.opentelemetry.io/otel/trace"

	"github.com/ruko1202/xhttp/internal/xtracer"
)

// SetTracerProvider configures the TracerProvider xhttp uses for its spans.
//
// Until it is called, xhttp traces into a no-op provider: importing the library
// does not change a service's tracing behavior, and a service that never
// configures tracing pays nothing for it. Call it once during startup, after
// building the provider:
//
//	tp := sdktrace.NewTracerProvider(...)
//	otel.SetTracerProvider(tp)
//	xhttp.SetTracerProvider(tp)
//
// Passing the same provider given to otel.SetTracerProvider is the usual setup;
// xhttp asks for it explicitly rather than reading the global one so that the
// dependency is visible at the call site.
//
// Spans are recorded under the instrumentation scope
// "github.com/ruko1202/xhttp" together with the module version, so they are
// attributable to this library in a trace UI rather than looking like part of
// the calling service.
//
// It is safe to call concurrently with in-flight requests.
func SetTracerProvider(tp trace.TracerProvider) {
	xtracer.SetTracerProvider(tp)
}
