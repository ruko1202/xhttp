package lifecycle

import "sync/atomic"

// ReadyChecker is the read-only view of start-up state that request handlers
// depend on. It mirrors DrainChecker: a readiness handler can hold traffic back
// until start-up has finished without being able to declare it finished.
type ReadyChecker interface {
	// IsReady reports whether main has declared start-up complete.
	IsReady() bool
}

// Starter is the single source of truth for whether this process has finished
// starting up. The zero value is ready to use, safe for concurrent access and
// NOT ready: a replica must opt in to traffic, never fall into it.
//
// Why an explicit flag rather than deriving readiness from the probes: during
// start-up the probes either pass too early (a module that has no probe is
// still initializing, so the load balancer sends traffic to a half-started
// replica) or fail (500 plus an error log line on every health-check poll for
// the whole start-up). Neither tells the truth. Only main knows when every
// module has started, so only main can say so — and then readiness does not
// depend on the order in which modules initialize.
//
// Ownership: main creates the Starter and is the only caller of MarkReady,
// once every module has started. Readers take it as a ReadyChecker.
//
// It is a separate type from Drainer on purpose. Folding a "started" flag into
// Drainer would leave every existing Drainer user that never calls MarkReady
// reporting not-ready forever.
type Starter struct {
	ready atomic.Bool
}

// NewStarter returns a Starter in the not-ready state.
func NewStarter() *Starter {
	return &Starter{}
}

// MarkReady declares start-up complete. The transition is one-way: there is no
// un-ready, because leaving the pool later is what draining is for.
// Idempotent and safe for concurrent use.
func (s *Starter) MarkReady() {
	s.ready.Store(true)
}

// IsReady implements ReadyChecker.
func (s *Starter) IsReady() bool {
	return s.ready.Load()
}
