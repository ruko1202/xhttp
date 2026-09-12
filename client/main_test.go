// This file holds every fixture and helper shared by the tests in this
// package — log capture, test servers, needles and the sample Sanitizer. The
// *_test.go files next to it contain assertions only.
package client_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/ruko1202/xlog"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

// secret is the needle. Assertions about redaction are about its presence or
// absence, so it must never appear in an expected value.
const secret = "S3CRETVALUE"

// observedContext returns a context carrying a logger whose every record is
// captured, plus a function rendering all of them as one string.
//
// The level is Debug on purpose: the transport logs exchanges at debug, and
// running these at info would assert that a silent logger stays silent —
// proving nothing while looking green.
func observedContext(t *testing.T) (ctx context.Context, dumpLogs func() string) {
	t.Helper()

	core, logs := observer.New(zapcore.DebugLevel)
	ctx = xlog.ContextWithLogger(context.Background(), xlog.NewZapAdapter(zap.New(core)))

	return ctx, func() string {
		var b strings.Builder
		for _, entry := range logs.All() {
			b.WriteString(entry.Message)
			// Fields carry the payload; encoding them as JSON keeps nested maps
			// (headers) searchable as plain text.
			encoded, err := json.Marshal(entry.ContextMap())
			require.NoError(t, err)
			b.Write(encoded)
			b.WriteString("\n")
		}

		return b.String()
	}
}

// recordedRequest captures what actually crossed the wire, so tests can assert
// on that rather than on what the client believes it sent.
type recordedRequest struct {
	header http.Header
	body   []byte
}

// echoServer replies 200 with a fixed JSON body plus a Set-Cookie carrying the
// secret, and records the request it saw.
//
// One instance serves one request at a time: the recording is unsynchronized,
// so pointing concurrent requests at a single instance is a data race the race
// detector will find. Give each parallel subtest its own.
func echoServer(t *testing.T) (srvURL string, seen *recordedRequest) {
	t.Helper()

	seen = &recordedRequest{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen.header = r.Header.Clone()
		seen.body, _ = io.ReadAll(r.Body)

		w.Header().Set("Set-Cookie", "session="+secret)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	t.Cleanup(srv.Close)

	return srv.URL, seen
}

// deadServerURL returns an address that is routable but has nothing listening,
// so a request to it fails inside the transport.
func deadServerURL(t *testing.T) string {
	t.Helper()

	dead := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	url := dead.URL
	dead.Close()

	return url
}

// redirectServer serves a 302 at /start pointing at /end, so redirect options
// can be judged by what the client actually did rather than by whether a
// function field is non-nil.
func redirectServer(t *testing.T) string {
	t.Helper()

	mux := http.NewServeMux()
	mux.HandleFunc("/start", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/end", http.StatusFound)
	})
	mux.HandleFunc("/end", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("arrived"))
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	return srv.URL
}

// mustDo issues a GET through httpc and returns the response body, closing the
// response.
func mustDo(ctx context.Context, t *testing.T, httpc *http.Client, url string) string {
	t.Helper()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	require.NoError(t, err)

	resp, err := httpc.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	return string(body)
}

const redacted = "[REDACTED]"

// maskingSanitizer is the kind of policy a service is expected to supply: it
// blanks a fixed set of header names and everything after the host in a URL.
type maskingSanitizer struct {
	mu   sync.Mutex
	body []byte // the body it was handed, for order-of-operations assertions
}

func (s *maskingSanitizer) SanitizeURL(u string) string {
	if scheme, rest, ok := strings.Cut(u, "://"); ok {
		if host, _, hasPath := strings.Cut(rest, "/"); hasPath {
			return scheme + "://" + host + "/" + redacted
		}
	}

	return u
}

func (s *maskingSanitizer) SanitizeHeaders(h http.Header) http.Header {
	out := make(http.Header, len(h))
	for name, values := range h {
		if name == "Authorization" || name == "Set-Cookie" {
			out[name] = []string{redacted}
			continue
		}

		out[name] = values
	}

	return out
}

// SanitizeBody records the LONGEST body it was handed. The transport sanitizes
// both the request and the response, so keeping the last one would report
// whichever happened to come second rather than the one under test.
func (s *maskingSanitizer) SanitizeBody(b []byte) []byte {
	s.mu.Lock()
	defer s.mu.Unlock()

	if len(b) > len(s.body) {
		s.body = append([]byte(nil), b...)
	}

	return []byte(redacted)
}

func (s *maskingSanitizer) seenBody() []byte {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.body
}
