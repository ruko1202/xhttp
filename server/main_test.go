// This file holds the fixtures shared by the tests in this package — log
// capture and a free-port helper. The *_test.go files next to it contain
// assertions only.
package server_test

import (
	"context"
	"net"
	"testing"

	"github.com/ruko1202/xlog"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

// observedContext returns a context carrying a logger whose every record is
// captured, plus accessors over what was logged.
//
// The level is Debug because the body-dump middleware logs at debug; running
// these at info would assert that a silent logger stays silent.
func observedContext(t *testing.T) (ctx context.Context, logs *observer.ObservedLogs) {
	t.Helper()

	core, recorded := observer.New(zapcore.DebugLevel)
	ctx = xlog.ContextWithLogger(context.Background(), xlog.NewZapAdapter(zap.New(core)))

	return ctx, recorded
}

// freePort asks the OS for a port and releases it, so a test server can bind a
// port nothing else is using. There is a race between the release and the
// bind, but it is the standard trade-off: Config has no "pick a port" mode,
// because the servers this package wraps always bind a configured address.
func freePort(t *testing.T) int {
	t.Helper()

	var lc net.ListenConfig
	l, err := lc.Listen(context.Background(), "tcp", "127.0.0.1:0")
	require.NoError(t, err)

	port := l.Addr().(*net.TCPAddr).Port
	require.NoError(t, l.Close())

	return port
}

// fieldsOf renders the fields of the single log entry with the given message,
// failing when there is not exactly one.
func fieldsOf(t *testing.T, logs *observer.ObservedLogs, message string) map[string]any {
	t.Helper()

	entries := logs.FilterMessage(message).All()
	require.Len(t, entries, 1, "expected exactly one %q entry", message)

	return entries[0].ContextMap()
}
