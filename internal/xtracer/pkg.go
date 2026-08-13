// Package xtracer manages the OpenTelemetry tracer used across xhttp.
//
// Spans are emitted under this module's import path as the instrumentation
// scope, so a trace shows the work as coming from the xhttp library rather than
// from whichever package inside the consuming service happens to call it.
package xtracer

import "runtime/debug"

const (
	// PkgName is the instrumentation scope reported to OpenTelemetry.
	PkgName = "github.com/ruko1202/xhttp"

	// defaultVersion is reported when build info is unavailable, which is the
	// normal case when the module is built from a local replace directive or
	// run under `go test`.
	defaultVersion = "v0.0.0"
)

// GetVersion returns the module version recorded in the binary's build info,
// falling back to defaultVersion when it cannot be determined.
func GetVersion() string {
	info, ok := debug.ReadBuildInfo()
	if ok {
		for _, dep := range info.Deps {
			if dep.Path == PkgName {
				return dep.Version
			}
		}
	}

	return defaultVersion
}
