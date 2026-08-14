package server_test

import (
	"context"
	"errors"
	"io"
	"maps"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/labstack/echo/v5"
	"github.com/stretchr/testify/require"

	"github.com/ruko1202/xhttp/server"
)

// requestLogKeys is the full field set of a REQUEST line. Dashboards and log
// queries are written against these names, so this list is a contract: a
// rename here breaks alerting downstream, which is why the test asserts the
// exact set rather than a subset.
var requestLogKeys = []string{
	"host",
	"request",
	"protocol",
	"status",
	"request_id",
	"query",
	"form-values",
	"latency",
	"bytes_in",
	"bytes_out",
	"remote_ip",
	"user_agent",
	"headers",
}

func TestBaseMiddlewares(t *testing.T) {
	t.Parallel()

	t.Run("recovers from a panicking handler", func(t *testing.T) {
		t.Parallel()

		e := echo.New()
		e.Use(server.BaseMiddlewares()...)
		e.GET("/boom", func(*echo.Context) error { panic("boom") })

		rec := serve(t, e, http.MethodGet, "/boom", nil)
		require.Equal(t, http.StatusInternalServerError, rec.Code)
	})

	t.Run("sets security headers", func(t *testing.T) {
		t.Parallel()

		e := echo.New()
		e.Use(server.BaseMiddlewares()...)
		e.GET("/ok", func(c *echo.Context) error { return c.NoContent(http.StatusNoContent) })

		rec := serve(t, e, http.MethodGet, "/ok", nil)
		require.NotEmpty(t, rec.Header().Get(echo.HeaderXContentTypeOptions))
	})
}

func TestRequestLoggingMiddleware(t *testing.T) {
	t.Parallel()

	t.Run("logs REQUEST with the full field set", func(t *testing.T) {
		t.Parallel()

		ctx, logs := observedContext(t)

		e := echo.New()
		e.Use(server.RequestLoggingMiddleware())
		e.GET("/ok", func(c *echo.Context) error { return c.NoContent(http.StatusNoContent) })

		serveCtx(ctx, t, e, http.MethodGet, "/ok", nil)

		fields := fieldsOf(t, logs, "REQUEST")
		require.ElementsMatch(t, requestLogKeys, keysOf(fields))
		require.EqualValues(t, http.StatusNoContent, fields["status"])
		require.Equal(t, "GET /ok", fields["request"])
	})

	t.Run("logs REQUEST_ERROR when the handler fails", func(t *testing.T) {
		t.Parallel()

		ctx, logs := observedContext(t)

		e := echo.New()
		e.Use(server.RequestLoggingMiddleware())
		e.GET("/fail", func(*echo.Context) error { return errors.New("nope") })

		serveCtx(ctx, t, e, http.MethodGet, "/fail", nil)

		fields := fieldsOf(t, logs, "REQUEST_ERROR")
		require.Subset(t, keysOf(fields), requestLogKeys)
		require.Contains(t, keysOf(fields), "error")
		require.Empty(t, logs.FilterMessage("REQUEST").All())
	})

	t.Run("skips swagger paths", func(t *testing.T) {
		t.Parallel()

		ctx, logs := observedContext(t)

		e := echo.New()
		e.Use(server.RequestLoggingMiddleware())
		e.GET("/swagger/index.html", func(c *echo.Context) error { return c.NoContent(http.StatusOK) })

		serveCtx(ctx, t, e, http.MethodGet, "/swagger/index.html", nil)

		require.Empty(t, logs.All(), "swagger assets must not reach the request log")
	})
}

func TestBodyDumpLoggingMiddleware(t *testing.T) {
	t.Parallel()

	t.Run("dumps request and response bodies", func(t *testing.T) {
		t.Parallel()

		ctx, logs := observedContext(t)

		e := echo.New()
		e.Use(server.BodyDumpLoggingMiddleware())
		e.POST("/echo", func(c *echo.Context) error { return c.String(http.StatusOK, "pong") })

		serveCtx(ctx, t, e, http.MethodPost, "/echo", strings.NewReader("ping"))

		require.Equal(t, "ping", fieldsOf(t, logs, "REQUEST DUMP")["body"])
		require.Equal(t, "pong", fieldsOf(t, logs, "RESPONSE DUMP")["body"])
	})

	t.Run("skips a response body past the cap", func(t *testing.T) {
		t.Parallel()

		ctx, logs := observedContext(t)

		big := strings.Repeat("x", 11*1024) // just over the 10Kb cap

		e := echo.New()
		e.Use(server.BodyDumpLoggingMiddleware())
		e.GET("/big", func(c *echo.Context) error { return c.String(http.StatusOK, big) })

		serveCtx(ctx, t, e, http.MethodGet, "/big", nil)

		require.Empty(t, logs.FilterMessage("RESPONSE DUMP").All())
		require.Len(t, logs.FilterMessageSnippet("RESPONSE DUMP skipped").All(), 1)
	})

	t.Run("falls back to the response request id", func(t *testing.T) {
		t.Parallel()

		ctx, logs := observedContext(t)

		// A tracing middleware that derives the id from the span writes it to
		// the response, not the request — the fallback is what keeps dumps
		// correlated with the rest of a request's logs.
		e := echo.New()
		e.Use(server.BodyDumpLoggingMiddleware())
		e.GET("/traced", func(c *echo.Context) error {
			c.Response().Header().Set(echo.HeaderXRequestID, "trace-abc")

			return c.String(http.StatusOK, "ok")
		})

		serveCtx(ctx, t, e, http.MethodGet, "/traced", nil)

		require.Equal(t, "trace-abc", fieldsOf(t, logs, "REQUEST DUMP")["request_id"])
	})
}

func keysOf(m map[string]any) []string {
	return slices.Collect(maps.Keys(m))
}

func serve(t *testing.T, e *echo.Echo, method, target string, body io.Reader) *httptest.ResponseRecorder {
	t.Helper()

	return serveCtx(context.Background(), t, e, method, target, body)
}

func serveCtx(
	ctx context.Context,
	t *testing.T,
	e *echo.Echo,
	method, target string,
	body io.Reader,
) *httptest.ResponseRecorder {
	t.Helper()

	req := httptest.NewRequestWithContext(ctx, method, target, body)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	return rec
}
