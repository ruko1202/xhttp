package server

import (
	"fmt"
	"strings"

	"github.com/labstack/echo/v5"
	"github.com/labstack/echo/v5/middleware"
	"github.com/ruko1202/xlog"
	"github.com/ruko1202/xlog/xfield"
)

// maxResponseDump caps the response body written to the debug log. Past this
// size the dump costs more than it explains, so only a note is logged.
const maxResponseDump = 10 * 1024 // 10Kb

// swaggerPathFragment marks requests excluded from request logging and body
// dumps: the Swagger UI pulls a stream of static assets and a spec that dwarfs
// every real payload, which would bury actual traffic in the log.
const swaggerPathFragment = "swagger"

// BaseMiddlewares returns the minimum every server wants: panic recovery and
// the standard security headers.
func BaseMiddlewares() []echo.MiddlewareFunc {
	return []echo.MiddlewareFunc{
		middleware.Recover(),
		middleware.Secure(),
	}
}

// RequestLoggingMiddleware logs one structured line per request.
//
// The field names and the REQUEST / REQUEST_ERROR message literals are a
// contract: log queries and alerts are written against them, so renaming one
// is a breaking change for every dashboard downstream.
func RequestLoggingMiddleware() echo.MiddlewareFunc {
	return middleware.RequestLoggerWithConfig(middleware.RequestLoggerConfig{
		LogLatency:       true,
		LogProtocol:      true,
		LogRemoteIP:      true,
		LogHost:          true,
		LogMethod:        true,
		LogURI:           true,
		LogURIPath:       false,
		LogRoutePath:     false,
		LogRequestID:     true,
		LogReferer:       false,
		LogUserAgent:     true,
		LogStatus:        true,
		LogContentLength: true,
		LogResponseSize:  true,
		// Nil, so Echo leaves RequestLoggerValues.Headers empty and the
		// "headers" field below always logs nothing. The field stays because
		// the key set is a contract that log queries are written against.
		// Populating this must be an allowlist of specific header names, never
		// every header — Authorization and Cookie are the ones you would get.
		LogHeaders:     nil,
		LogQueryParams: nil,
		LogFormValues:  nil,
		// Forwards the error to the global error handler, so it can decide the
		// appropriate status code.
		HandleError: true,
		Skipper:     skipper(swaggerPathFragment),
		LogValuesFunc: func(c *echo.Context, v middleware.RequestLoggerValues) error {
			ctx := c.Request().Context()
			attrs := []xfield.Field{
				xfield.String("host", v.Host),
				xfield.String("request", fmt.Sprintf("%s %s", v.Method, v.URI)),
				xfield.String("protocol", v.Protocol),
				xfield.Int("status", v.Status),
				xfield.String("request_id", v.RequestID),
				xfield.Any("query", v.QueryParams),
				xfield.Any("form-values", v.FormValues),
				xfield.Duration("latency", v.Latency),
				xfield.String("bytes_in", v.ContentLength),
				xfield.Int64("bytes_out", v.ResponseSize),
				xfield.String("remote_ip", v.RemoteIP),
				xfield.String("user_agent", v.UserAgent),
				xfield.Any("headers", v.Headers),
			}

			if v.Error != nil {
				xlog.Error(ctx, "REQUEST_ERROR", append(attrs, xfield.Error(v.Error))...)

				return nil //nolint:nilerr
			}

			xlog.Info(ctx, "REQUEST", attrs...)

			return nil
		},
	})
}

// BodyDumpLoggingMiddleware logs request and response bodies at debug level.
//
// It writes bodies verbatim: passwords from a login, refresh tokens, and
// whatever secrets an integration endpoint accepts all land in the log in
// clear text. There is no field redaction. Enable it only where the logs live
// no longer than the debugging session — behind a dev gate, never in an
// environment whose logs are shipped somewhere and retained.
//
// The response body is capped; the request body is not, and a large upload is
// buffered whole in memory before being logged.
func BodyDumpLoggingMiddleware() echo.MiddlewareFunc {
	return middleware.BodyDumpWithConfig(middleware.BodyDumpConfig{
		Skipper: skipper(swaggerPathFragment),
		Handler: func(c *echo.Context, reqDump []byte, respDump []byte, _ error) {
			req := c.Request()
			res := c.Response()
			ctx := req.Context()

			// The request id is read from the request first and the response
			// second: a tracing middleware that derives the id from the span
			// writes it to the response header, so the fallback is what keeps
			// dumps correlated with the rest of the request's logs.
			requestID := req.Header.Get(echo.HeaderXRequestID)
			if requestID == "" {
				requestID = res.Header().Get(echo.HeaderXRequestID)
			}

			attrs := []xfield.Field{
				xfield.String("method", req.Method),
				xfield.String("uri", req.RequestURI),
				xfield.String("request_id", requestID),
			}

			xlog.Debug(ctx, "REQUEST DUMP", append(attrs, xfield.String("body", string(reqDump)))...)
			if len(respDump) > maxResponseDump {
				xlog.Info(
					ctx,
					fmt.Sprintf("RESPONSE DUMP skipped: too large response body [%d Kb]", len(respDump)/1024),
					attrs...,
				)

				return
			}
			xlog.Debug(ctx, "RESPONSE DUMP", append(attrs, xfield.String("body", string(respDump)))...)
		},
	})
}

func skipper(urlPaths ...string) middleware.Skipper {
	return func(c *echo.Context) bool {
		for _, path := range urlPaths {
			if strings.Contains(c.Request().URL.Path, path) {
				return true
			}
		}

		return false
	}
}
