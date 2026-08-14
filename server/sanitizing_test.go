package server_test

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/labstack/echo/v5"
	"github.com/stretchr/testify/require"

	"github.com/ruko1202/xhttp/sanitize"
	"github.com/ruko1202/xhttp/server"
)

// redactingSanitizer is the kind of policy a real service writes: it keeps the
// path and drops the query, which is what an OAuth callback needs — the code and
// state live in the query and are live credentials.
type redactingSanitizer struct{}

func (redactingSanitizer) SanitizeURL(u string) string {
	parsed, err := url.Parse(u)
	if err != nil {
		return "[UNPARSEABLE]"
	}
	if parsed.RawQuery == "" {
		return parsed.Path
	}

	return parsed.Path + "?[REDACTED]"
}

func (redactingSanitizer) SanitizeHeaders(h http.Header) http.Header {
	out := make(http.Header, len(h))
	for name, values := range h {
		if name == "Authorization" || name == "Cookie" {
			out[name] = []string{"[REDACTED]"}

			continue
		}
		out[name] = values
	}

	return out
}

func (redactingSanitizer) SanitizeBody([]byte) []byte { return []byte("[REDACTED]") }

func TestRequestLoggingMiddlewareWithSanitizer(t *testing.T) {
	t.Parallel()

	t.Run("redacts the query string out of the logged URI", func(t *testing.T) {
		t.Parallel()

		ctx, logs := observedContext(t)

		e := echo.New()
		e.Use(server.RequestLoggingMiddlewareWithSanitizer(redactingSanitizer{}))
		e.GET("/callback", func(c *echo.Context) error { return c.NoContent(http.StatusNoContent) })

		serveCtx(ctx, t, e, http.MethodGet, "/callback?code=live-secret&state=xyz", nil)

		fields := fieldsOf(t, logs, "REQUEST")
		require.Equal(t, "GET /callback?[REDACTED]", fields["request"])

		// The whole point: the credential must not survive anywhere in the record.
		for key, value := range fields {
			require.NotContains(t, fmt.Sprint(value), "live-secret",
				"field %q leaked the authorization code", key)
		}
	})

	t.Run("nil sanitizer behaves as the no-op and does not panic", func(t *testing.T) {
		t.Parallel()

		ctx, logs := observedContext(t)

		e := echo.New()
		e.Use(server.RequestLoggingMiddlewareWithSanitizer(nil))
		e.GET("/ok", func(c *echo.Context) error { return c.NoContent(http.StatusNoContent) })

		require.NotPanics(t, func() {
			serveCtx(ctx, t, e, http.MethodGet, "/ok?keep=me", nil)
		})

		fields := fieldsOf(t, logs, "REQUEST")
		require.Equal(t, "GET /ok?keep=me", fields["request"])
	})

	t.Run("the plain constructor still redacts nothing", func(t *testing.T) {
		t.Parallel()

		ctx, logs := observedContext(t)

		e := echo.New()
		e.Use(server.RequestLoggingMiddleware())
		e.GET("/ok", func(c *echo.Context) error { return c.NoContent(http.StatusNoContent) })

		serveCtx(ctx, t, e, http.MethodGet, "/ok?token=verbatim", nil)

		fields := fieldsOf(t, logs, "REQUEST")
		require.Equal(t, "GET /ok?token=verbatim", fields["request"])
	})
}

func TestBodyDumpLoggingMiddlewareWithSanitizer(t *testing.T) {
	t.Parallel()

	t.Run("redacts both dumps", func(t *testing.T) {
		t.Parallel()

		ctx, logs := observedContext(t)

		e := echo.New()
		e.Use(server.BodyDumpLoggingMiddlewareWithSanitizer(redactingSanitizer{}))
		e.POST("/login", func(c *echo.Context) error {
			return c.String(http.StatusOK, `{"session":"response-secret"}`)
		})

		serveCtx(ctx, t, e, http.MethodPost, "/login", strings.NewReader(`{"password":"request-secret"}`))

		reqDump := fieldsOf(t, logs, "REQUEST DUMP")
		require.Equal(t, "[REDACTED]", reqDump["body"])

		respDump := fieldsOf(t, logs, "RESPONSE DUMP")
		require.Equal(t, "[REDACTED]", respDump["body"])
	})

	t.Run("nil sanitizer behaves as the no-op and does not panic", func(t *testing.T) {
		t.Parallel()

		ctx, logs := observedContext(t)

		e := echo.New()
		e.Use(server.BodyDumpLoggingMiddlewareWithSanitizer(nil))
		e.POST("/echo", func(c *echo.Context) error { return c.String(http.StatusOK, "pong") })

		require.NotPanics(t, func() {
			serveCtx(ctx, t, e, http.MethodPost, "/echo", strings.NewReader("ping"))
		})

		require.Equal(t, "ping", fieldsOf(t, logs, "REQUEST DUMP")["body"])
		require.Equal(t, "pong", fieldsOf(t, logs, "RESPONSE DUMP")["body"])
	})
}

// TestSanitizerTypesAreInterchangeable states at compile time what the alias in
// the client package buys: one policy value serves both halves of the library.
func TestSanitizerTypesAreInterchangeable(t *testing.T) {
	t.Parallel()

	var policy sanitize.Sanitizer = redactingSanitizer{}
	require.NotNil(t, server.RequestLoggingMiddlewareWithSanitizer(policy))
	require.NotNil(t, server.BodyDumpLoggingMiddlewareWithSanitizer(policy))
}
