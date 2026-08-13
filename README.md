# xhttp

HTTP building blocks for Go services, with structured logging and tracing wired
in through [xlog](https://github.com/ruko1202/xlog).

```bash
go get github.com/ruko1202/xhttp
```

## `client`

An `*http.Client` that logs and traces every exchange. It is an ordinary
`*http.Client`, so it drops into any library that accepts one.

```go
import "github.com/ruko1202/xhttp/client"

httpc := client.NewClient(
    client.WithTimeout(5 * time.Second),
    client.WithSanitizer(mySanitizer),
)

resp, err := httpc.Do(req) // logged and traced
```

Each request opens a span and emits a debug record on the way out and on the way
back, carrying host, method, URL, protocol, status and headers. Because the span
lives in the request context, log records made further down the call inherit the
same fields — and outgoing calls line up under the operation that made them in a
trace.

### Tracing

Spans are recorded under the instrumentation scope `github.com/ruko1202/xhttp`
along with the module version, so a trace attributes them to this library rather
than to whichever package in your service happened to call it.

Until a provider is configured, xhttp traces into a no-op: importing it does not
change your tracing setup, and a service that does not trace pays nothing for
it. Wire it once at startup:

```go
tp := sdktrace.NewTracerProvider( /* ... */ )
otel.SetTracerProvider(tp)
xhttp.SetTracerProvider(tp)
```

The provider is passed explicitly rather than read from the global one, so the
dependency is visible at the call site. Clients built before this call keep the
no-op tracer, so configure it before constructing them.

### Redaction is opt-in

> [!WARNING]
> The default sanitizer redacts **nothing**. A client built without
> `WithSanitizer` writes URLs and headers to the log verbatim — `Authorization`,
> `Cookie`, session tokens, and any credential carried in a URL path or query.

This is deliberate: what counts as a secret is service knowledge, so the library
supplies the seam and you supply the policy. If your requests carry credentials,
pass a sanitizer:

```go
type redactor struct{}

func (redactor) SanitizeURL(u string) string { /* mask the path and query */ }

func (redactor) SanitizeHeaders(h http.Header) http.Header {
    out := make(http.Header, len(h))
    for name, values := range h {
        if name == "Authorization" || name == "Cookie" {
            out[name] = []string{"[REDACTED]"}
            continue
        }
        out[name] = values
    }
    return out // a copy — never mutate the argument
}

func (redactor) SanitizeBody(b []byte) []byte { return b }
```

A `Sanitizer` must not mutate its argument: `http.Client` re-enters `RoundTrip`
with the same `*http.Request` on retries and redirects, so a mutating sanitizer
corrupts the second attempt.

### Options

| Option | Effect |
|---|---|
| `WithTimeout(d)` | Total request timeout. Non-positive values are ignored; default 30s. |
| `WithSanitizer(s)` | Redaction policy for logged URLs, headers and bodies. `nil` is ignored. |
| `WithTransport(tr)` | Replaces the underlying `*http.Transport`, keeping logging. |
| `WithoutRedirect()` | Returns the redirect response instead of following it. |
| `WithCustomRedirectFlow(f)` | Custom `CheckRedirect` policy. |
| `WithCallerBeforeDo(f)` | Hook run before the request is sent — and before it is logged. |
| `WithCallerAfterDo(f)` | Hook run after a successful round-trip. |
| `WithBodyLogging()` | Dumps request and response bodies at debug level. |

There is no bearer-token option: authentication is the caller's concern, and
`WithCallerBeforeDo` is all it takes.

```go
client.WithCallerBeforeDo(func(_ context.Context, r *http.Request) {
    r.Header.Set("Authorization", "Bearer "+token)
})
```

Hooks run before the request is logged, so headers they add are visible to the
sanitizer and are therefore actually subject to redaction.

### Body logging

> [!CAUTION]
> `WithBodyLogging` writes full request and response bodies to the log. Bodies
> are the one thing a sanitizer struggles to make safe — their shape is
> arbitrary, so a secret inside one is unrecognizable. Treat it as a local
> debugging tool.

Dumps are capped at 4 KiB and marked with `…[TRUNCATED]`; the request still goes
out in full. Consider forbidding the option outside tests, e.g. with
golangci-lint's `forbidigo`:

```yaml
forbidigo:
  analyze-types: true
  forbid:
    - pattern: '^github\.com/ruko1202/xhttp/client\.WithBodyLogging$'
      msg: body logging is a local debugging tool; never enable it in shipped code
```

## Defaults

| Setting | Value |
|---|---|
| Request timeout | 30s |
| Dial timeout / keep-alive | 5s / 30s |
| TLS handshake timeout | 5s |
| Response header timeout | 10s |
| Expect-continue timeout | 1s |
| Idle connection timeout | 90s |
| Max idle connections | 1000 (100 per host) |
| Proxy | `http.ProxyFromEnvironment` |
| HTTP/2 | attempted |
| Body log cap | 4 KiB |

## License

MIT — see [LICENSE](./LICENSE).
