package infra

import (
	"strconv"

	"github.com/ruko1202/xhttp/server"
)

// Server is the infra HTTP server. Construct it with New; it registers every
// route itself, so there is nothing to bind afterwards.
type Server struct {
	*server.Server

	cfg Config
}

// noopDrainChecker stands in for an absent Drainer, so the readiness handler
// can consult it unconditionally instead of nil-guarding on every request.
type noopDrainChecker struct{}

func (noopDrainChecker) IsDraining() bool { return false }

// New creates the infra server and registers its routes.
//
// Options are passed through to the underlying HTTP shell, so WithLogger,
// WithEcho and WithNotFoundHandler all apply here.
//
// It panics on a Check with no Probe. That is a wiring mistake, and the two
// alternatives are worse: skipping the check silently would leave a replica
// reporting ready forever without having verified anything, and failing the
// probe at request time would turn a typo into an outage discovered in
// production rather than at startup.
func New(cfg Config, opts ...server.Option) *Server {
	if cfg.Drainer == nil {
		cfg.Drainer = noopDrainChecker{}
	}

	for _, check := range cfg.Checks {
		if check.Probe == nil {
			panic("xhttp/infra: readiness check " + strconv.Quote(check.Name) + " has no Probe")
		}
	}

	s := &Server{
		Server: server.New(cfg.Server, opts...),
		cfg:    cfg,
	}

	s.bindRouters()

	return s
}
