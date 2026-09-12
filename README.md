# xhttp

HTTP building blocks for Go services, with structured logging and tracing wired
in through [xlog](https://github.com/ruko1202/xlog).

```bash
go get github.com/ruko1202/xhttp
```

Six packages, imported separately so you only pay for what you use — `client`
is plain `net/http` and does not pull Echo into your binary:

| Package | What it gives you |
|---|---|
| [`client`](#client) | an `*http.Client` that logs and traces every exchange |
| [`server`](#server) | the Echo v5 lifecycle shell: bind, start, stop, request logging |
| [`infra`](#infra) | a whole infra server — health, version, metrics, pprof, swagger |
| [`lifecycle`](#lifecycle) | the graceful-drain signal that makes readiness meaningful |
| [`sanitize`](#sanitize) | the redaction policy `client` and `server` both log through |
| [`dialguard`](#dialguard) | the address policy that stops a client dialing inside your network |

Requires Go 1.25 and, for `server`/`infra`, Echo v5.2.1 or newer. The Echo
dependency is deliberate and unabstracted: `server.Echo()` hands you the real
instance to register routes and middlewares on.

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

The interface lives in [`sanitize`](#sanitize), so one policy value serves both
the outgoing client and the inbound request logger.

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
| `WithProxy(f)` | Overrides proxy selection, replacing `http.ProxyFromEnvironment`. `nil` is ignored. |
| `WithDialGuard(g)` | Refuses a connection when `g` rejects the resolved address. `nil` is ignored. |
| `WithoutInternalHosts()` | `WithDialGuard` over the default deny list. Off unless asked for. |

There is no bearer-token option: authentication is the caller's concern, and
`WithCallerBeforeDo` is all it takes.

```go
client.WithCallerBeforeDo(func(_ context.Context, r *http.Request) {
    r.Header.Set("Authorization", "Bearer "+token)
})
```

Hooks run before the request is logged, so headers they add are visible to the
sanitizer and are therefore actually subject to redaction.

### Guarding where the client may dial

When a destination comes from outside the service — an issuer URL an
administrator types into a form, a callback address in a payload — the fetch is
an SSRF primitive: it can be aimed at `169.254.169.254`, at an admin port on
loopback, or at a neighbor on a private range. `WithoutInternalHosts` refuses
those connections.

```go
httpc := client.NewClient(
    client.WithoutInternalHosts(),
    client.WithSanitizer(mySanitizer),
)
```

It is **off by default** and should stay off where the destination is your own:
xhttp is also how services reach each other inside the perimeter, and a guard
enabled by default would break that silently.

The check runs in `net.Dialer.Control`, on the resolved address, immediately
before `connect(2)`. That placement is the point. Validating the URL string
cannot work — measured on go1.25.13, `127.1`, `2130706433`, `0x7f000001` and
`017700000001` are all rejected by `net.ParseIP` and `netip.ParseAddr`, and all
resolved to `127.0.0.1` by the resolver, so a string check lives in the gap
between them. And because the guard runs per connection — redirects, retries,
and once per address the resolver returned — it also closes DNS rebinding,
which a check made when a URL is *stored* never can.

For a policy of your own, build one from `dialguard` and pass it to
`WithDialGuard`:

```go
// The default list, minus one network you do reach on purpose. DeleteFunc
// edits in place, which is safe here precisely because InternalPrefixes hands
// out a slice of its own each time.
allowed := slices.DeleteFunc(dialguard.InternalPrefixes(), func(p netip.Prefix) bool {
    return p == netip.MustParsePrefix("10.0.0.0/8")
})

httpc := client.NewClient(client.WithDialGuard(dialguard.Blocking(allowed...)))
```

`WithDialGuard` takes any `func(network, address string) error`, so an
allow-list is a few lines over `dialguard.IsInternal`.

#### What it does not cover

Worth knowing before relying on it — a guard whose blind spots are undocumented
is worse than no guard, because you stop looking.

- **An HTTP proxy bypasses it.** The default transport honors
  `http.ProxyFromEnvironment`. With a proxy set, the dialer connects to the
  *proxy* — the only address the guard ever sees — while the real target rides
  in the request itself: the request line for `http`, a `CONNECT` for `https`.
  `NO_PROXY` makes that partial rather than all-or-nothing: destinations it
  exempts are dialed directly and are guarded.

- **`WithTransport` removes it, in either option order.** Replacing the
  transport discards the dialer the guard lives on; applied afterwards, the
  option no longer finds the wrapper it needs. The client looks configured and
  is not.
- **A denial refuses one address, not the dial.** When a name resolves to
  several addresses, the dialer treats a refusal as a failed attempt and tries
  the next one — so a policy that covers IPv4 but not IPv6 (or the reverse)
  blocks nothing at all. `WithoutInternalHosts` is safe here because its list
  covers both families of every range; a hand-built list must do the same.
- **It runs per connection, not per request.** A connection the guard approved
  stays in the pool (`IdleConnTimeout` is 90s) and serves later requests
  without being consulted again.
- **The error names an address, not a host.** The guard sees only what the
  resolver returned, so a message telling an operator *which URL* was rejected
  has to come from your code. Validating the URL on input is worth doing for
  that reason — as a second layer, not a replacement.

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

## `server`

The lifecycle shell around an Echo v5 instance. It owns no routes and no
middleware chain — those are yours. What it owns is the part every service
would otherwise rewrite: binding the configured address, starting Echo so it
unblocks on context cancellation, and shutting down.

```go
import xhttpserver "github.com/ruko1202/xhttp/server"

srv := xhttpserver.New(xhttpserver.Config{
    Name: "api",
    Host: "0.0.0.0",
    Port: 8000,
}, xhttpserver.WithLogger(slogLogger))

srv.Echo().Use(xhttpserver.BaseMiddlewares()...)
srv.Echo().Use(xhttpserver.RequestLoggingMiddleware())
srv.Echo().GET("/hello", helloHandler)

if err := srv.Start(ctx); err != nil { // blocks
    return err
}
```

`Echo()` is for configuration between `New` and `Start`; mutating the router
afterwards is unsupported.

> **`Stop` signals, it does not wait.** It cancels the context `Start` blocks
> on and returns immediately, leaving Echo to drain in the background. If you
> tear down dependencies right after `Stop` returns, you can close a database
> out from under a request still being served — hold your own drain window
> first.

`RequestLoggingMiddleware` writes one structured line per request;
`BodyDumpLoggingMiddleware` adds request/response bodies at debug level, capped
at 10 KiB, and is meant for development because it buffers every body in
memory. Both skip paths containing `swagger`, whose asset stream would drown
real traffic.

## `infra`

A complete infra server that starts itself and owns its routes. That is the
point: these paths are what proxy health checks, Prometheus and profile
scrapers pin themselves to, so every service should expose the same ones rather
than each wiring its own.

```go
import (
    "github.com/ruko1202/xhttp/infra"
    "github.com/ruko1202/xhttp/lifecycle"
)

drainer := lifecycle.NewDrainer()

srv := infra.New(infra.Config{
    Server:  xhttpserver.Config{Name: "infra", Host: "0.0.0.0", Port: 8001},
    Dev:     cfg.IsDev(),
    Version: infra.VersionInfo{AppName: "myservice", Version: version},
    Checks:  []infra.Check{{Name: "postgres", Probe: db.PingContext}},
    Drainer: drainer,
})

go srv.Start(ctx) // routes are already registered
```

| Route | Behaviour |
|---|---|
| `GET /liveness` | 204, unconditionally — checks nothing on purpose, so a database blip cannot cause a restart loop |
| `GET /readiness` | 204 ready · 503 draining · 500 a check failed |
| `GET /version` | 200, the `VersionInfo` you supplied |
| `GET /metrics` | 200, the Prometheus default registry |
| `GET /debug/pprof/*` | the standard profiles, in **every** environment |
| `GET /` | index page — `Dev` only |
| `GET /swagger/*` | Swagger UI — `Dev` only, and only when `Specs` is non-empty |

Readiness checks drain **before** probes, and that ordering is a contract: 503
means "planned drain", 500 means "a dependency is down". A draining replica
whose database is already closed must not report an outage during a routine
deploy.

Only the not-found route is request-logged. Health checkers poll every couple
of seconds forever; logging them buries everything else.

> **Exposure.** None of these endpoints is authenticated, and `/debug/pprof`
> serves heap dumps — memory contents, which can include tokens held in
> buffers. Run this on a port that is not published past your perimeter.
> `infra` cannot enforce that; it is yours to get right.

A `Check` with a nil `Probe` makes `New` panic. A wiring typo must not become a
replica that reports ready forever without having verified anything.

## `sanitize`

`Sanitizer`, the interface both halves of this library log through, plus the
no-op default. It is its own package for the same reason `lifecycle` is: it
imports nothing but `net/http`, so a service can depend on the policy type
without pulling in either Echo or the transport stack.

```go
import "github.com/ruko1202/xhttp/sanitize"

var _ sanitize.Sanitizer = myRedactor{}

httpc := client.NewClient(client.WithSanitizer(myRedactor{}))
mw := server.RequestLoggingMiddlewareWithSanitizer(myRedactor{})
```

There is deliberately one name for this type rather than a per-package alias:
a service that redacts credentials wants the same policy applied on the way out
and on the way in, and two names for one contract only invite them to drift.

## `dialguard`

The address policy `client` dials through, as a package of its own so a service
can name one without importing `client`.

```go
import "github.com/ruko1202/xhttp/dialguard"

guard := dialguard.Blocking(dialguard.InternalPrefixes()...)
err := guard("tcp4", "169.254.169.254:80") // errors.Is(err, dialguard.ErrBlockedAddress)
```

| Symbol | What it is |
|---|---|
| `Guard` | `func(network, address string) error` — the shape `WithDialGuard` takes. |
| `ErrBlockedAddress` | Wrapped by every denial, so `errors.Is` tells a block from a timeout. |
| `InternalPrefixes()` | The default deny list, a fresh slice per call. |
| `IsInternal(addr)` | Whether an address falls in that list. |
| `Blocking(prefixes...)` | A `Guard` denying anything inside `prefixes`. |

`InternalPrefixes` is explicit prefixes rather than the `net.IP` predicates,
because those cover less than they look like they do: `IsPrivate` is only
RFC1918 and `fc00::/7`, `IsUnspecified` matches `0.0.0.0` but not the
`0.0.0.0/8` around it, and four IPv6 forms that carry an IPv4 target inside them
satisfy no predicate at all. `::ffff:0:7f00:1` is the one to know: it is
`127.0.0.1` in IPv4-*translated* form, it is not what `::ffff:127.0.0.1` is,
`Unmap` does nothing to it, and only an explicit `::ffff:0:0:0/96` stops it.

`Blocking` parses before it consults the list, so an address it cannot
understand is refused whatever the list holds — including a `unix` socket path.
A guard that fails open on input it does not understand is one an attacker only
has to confuse.

## `lifecycle`

The drain signal, in its own package because it imports nothing but
`sync/atomic` — a service that owns drain state but runs no infra port can
depend on it without pulling in Echo.

```go
drainer := lifecycle.NewDrainer() // main owns it
drainer.StartDraining()           // from the shutdown path; one-way
```

Handlers take the read-only `lifecycle.DrainChecker` view, so a request handler
can report "not ready" without owning process-shutdown state.

## Defaults

`client`:

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
| SSRF dial guard | off (see `WithoutInternalHosts`) |

`server`:

| Setting | Value |
|---|---|
| Graceful timeout | 10s (`Config.GracefulTimeout`) |
| Response dump cap | 10 KiB |
| Log-skipped paths | those containing `swagger` |

## License

MIT — see [LICENSE](./LICENSE).
