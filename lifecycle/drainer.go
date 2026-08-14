// Package lifecycle holds process-lifecycle state shared between a service's
// main function and its HTTP layer — currently the graceful-drain signal that
// powers zero-downtime rolling deploys.
//
// Ownership: main creates the Drainer and is the only caller of StartDraining
// (from its shutdown path). Readers (e.g. a readiness handler) take it as a
// narrow DrainChecker interface, so a request handler never owns or mutates
// process-shutdown state.
//
// The package deliberately imports nothing beyond sync/atomic: a service that
// owns drain state but runs no infra server can depend on it without pulling
// in an HTTP framework.
package lifecycle

import "sync/atomic"

// DrainChecker is the read-only view of drain state that request handlers
// depend on. Keeping handlers behind this interface means they can report
// "not ready" without knowing anything about how shutdown is orchestrated.
type DrainChecker interface {
	// IsDraining reports whether the process has begun graceful shutdown.
	IsDraining() bool
}

// Drainer is the single source of truth for whether this process is draining.
// The zero value is ready to use and safe for concurrent access.
type Drainer struct {
	draining atomic.Bool
}

// NewDrainer returns a Drainer in the not-draining state.
func NewDrainer() *Drainer {
	return &Drainer{}
}

// StartDraining marks the process as draining. The transition is one-way:
// there is no un-drain, because draining only ever precedes process exit.
// Idempotent and safe for concurrent use.
func (d *Drainer) StartDraining() {
	d.draining.Store(true)
}

// IsDraining implements DrainChecker.
func (d *Drainer) IsDraining() bool {
	return d.draining.Load()
}
