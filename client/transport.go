package client

import (
	"context"
	"net"
	"net/http"
	"time"

	"github.com/ruko1202/xlog"
	"github.com/ruko1202/xlog/xfield"
	"go.opentelemetry.io/otel/trace"

	"github.com/ruko1202/xhttp/sanitize"

	"github.com/ruko1202/xhttp/internal/xtracer"
)

// transport is the logging round-tripper wrapping a standard *http.Transport.
// It owns the redaction policy and the caller hooks; the options that configure
// them find it by type-asserting http.Client.Transport.
type transport struct {
	tr              *http.Transport
	beforeRoundTrip []func(context.Context, *http.Request)
	afterRoundTrip  []func(context.Context, *http.Response)
	sanitizer       sanitize.Sanitizer
	tracer          trace.Tracer
	// logBodies is off unless a caller opts in with WithBodyLogging. Bodies are
	// the hardest thing to redact — their shape is arbitrary, so a secret inside
	// one is unrecognizable. Not writing them at all is the safe default.
	logBodies bool
}

func newTransport() *transport {
	dialer := &net.Dialer{
		// Time allowed to establish the TCP connection. 5s is generous even for
		// intercontinental round-trips.
		Timeout:   5 * time.Second,
		KeepAlive: 30 * time.Second,
	}

	tr := &transport{
		tr: &http.Transport{
			Proxy:             http.ProxyFromEnvironment,
			ForceAttemptHTTP2: true,
			// Connection pool sizing, so a busy client reuses connections
			// instead of dialing on every call.
			MaxIdleConns:        1000,
			MaxIdleConnsPerHost: 100,
			DialContext:         dialer.DialContext,
			// Time allowed for the TLS handshake; matters on lossy networks.
			TLSHandshakeTimeout: 5 * time.Second,
			// Caps the wait for response headers. Guards against a server that
			// accepts the connection and then thinks forever.
			ResponseHeaderTimeout: 10 * time.Second,
			// Wait for 100-continue before sending a large body.
			ExpectContinueTimeout: 1 * time.Second,
			IdleConnTimeout:       90 * time.Second,
		},
		sanitizer: sanitize.NewNoopSanitizer(),
		// Resolved once at construction. A client built before
		// xhttp.SetTracerProvider keeps the no-op tracer, which is the quiet
		// direction to be wrong in.
		tracer: xtracer.GetTracer(),
	}

	// Logging is itself a hook, registered first so caller hooks observe the
	// same ordering guarantees the logging one relies on.
	tr.beforeRoundTrip = append(tr.beforeRoundTrip, tr.logRequest)
	//nolint:bodyclose // registers an after-response hook; it neither owns nor reads the body, so there is nothing to close here.
	tr.afterRoundTrip = append(tr.afterRoundTrip, tr.logResponse)

	return tr
}

// RoundTrip logs and traces the exchange, then delegates to the wrapped
// transport.
//
// The span fields matter more than they look: WithOperationSpan attaches them
// to the logger, so they reappear on every later record made with this context
// — including the error below, which is emitted at a level that ships in
// production. That is why the URL is sanitized here rather than merely omitted:
// req.RequestURI is always empty on a client request, and the obvious
// substitution of req.URL.String() would write any credential living in the
// path or query straight to the log.
func (t *transport) RoundTrip(req *http.Request) (*http.Response, error) {
	ctx, span := xlog.WithOperationSpan(t.traceCtx(req.Context()), "xhttp.client.RoundTrip",
		xfield.String("host", req.Host),
		xfield.String("method", req.Method),
		xfield.String("url", t.sanitizer.SanitizeURL(req.URL.String())),
	)
	defer span.End()

	t.doBeforeRoundTrip(ctx, req)

	resp, err := t.tr.RoundTrip(req)
	if err != nil {
		// Safe as-is: *url.Error, which embeds the unredacted URL, is built by
		// http.Client.do above this transport. Here err is a bare *net.OpError
		// with no URL in it.
		xlog.Error(ctx, "failed to execute request", xfield.Error(err))
		return nil, err
	}

	t.doAfterRoundTrip(ctx, resp)

	return resp, err
}

// traceCtx installs this library's tracer, so spans below are attributed to
// xhttp rather than to whatever instrumentation scope the caller is using.
func (t *transport) traceCtx(ctx context.Context) context.Context {
	return xlog.ContextWithTracer(ctx, t.tracer)
}

func (t *transport) doBeforeRoundTrip(ctx context.Context, req *http.Request) {
	_, span := xlog.WithOperationSpan(ctx, "xhttp.client.beforeRoundTrip")
	defer span.End()

	for _, f := range t.beforeRoundTrip {
		f(ctx, req)
	}
}

func (t *transport) doAfterRoundTrip(ctx context.Context, resp *http.Response) {
	_, span := xlog.WithOperationSpan(ctx, "xhttp.client.afterRoundTrip")
	defer span.End()

	for _, f := range t.afterRoundTrip {
		f(ctx, resp)
	}
}

func (t *transport) logRequest(ctx context.Context, req *http.Request) {
	bodyLogField := xfield.String("body", bodyLoggingDisabled)

	if t.logBodies {
		bodyLogField = xfield.Any("body", truncateForLog(t.sanitizer.SanitizeBody(dumpRequestBody(req))))
	}

	xlog.Debug(ctx, "sending request",
		xfield.String("protocol", req.Proto),
		xfield.Any("headers", t.sanitizer.SanitizeHeaders(req.Header)),
		bodyLogField,
	)
}

func (t *transport) logResponse(ctx context.Context, resp *http.Response) {
	bodyLogField := xfield.String("body", bodyLoggingDisabled)

	if t.logBodies {
		bodyLogField = xfield.Any("body", truncateForLog(t.sanitizer.SanitizeBody(dumpResponseBody(resp))))
	}

	xlog.Debug(ctx, "received response",
		xfield.String("protocol", resp.Proto),
		xfield.String("status", resp.Status),
		xfield.Any("headers", t.sanitizer.SanitizeHeaders(resp.Header)),
		bodyLogField,
	)
}
