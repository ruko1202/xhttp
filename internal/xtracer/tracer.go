package xtracer

import (
	"sync"

	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/noop"
)

// The provider defaults to a no-op: a library that has not been given a tracer
// must stay silent rather than reach for the global one, so importing xhttp
// cannot change a service's tracing behavior on its own.
var (
	_mu                   sync.RWMutex
	_globalTracerProvider trace.TracerProvider = noop.NewTracerProvider()
	tracer                                     = initTracer(_globalTracerProvider)
)

// SetTracerProvider sets the tracer provider used by xhttp. It is safe for
// concurrent use.
func SetTracerProvider(tp trace.TracerProvider) {
	_mu.Lock()
	defer _mu.Unlock()

	_globalTracerProvider = tp
	tracer = initTracer(tp)
}

// GetTracer returns the tracer built from the current provider. It is safe for
// concurrent use.
func GetTracer() trace.Tracer {
	_mu.RLock()
	defer _mu.RUnlock()

	return tracer
}

func initTracer(tp trace.TracerProvider) trace.Tracer {
	return tp.Tracer(
		PkgName,
		trace.WithInstrumentationVersion(GetVersion()),
	)
}
