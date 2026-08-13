package infra

import (
	"bytes"
	"fmt"
	"html/template"
	"net/http"

	"github.com/labstack/echo/v5"
)

// mainPageTemplate is an index of this server's endpoints, kept deliberately
// plain: it is an operator's landing page on an internal port, not a product
// surface. The service name comes from VersionInfo, so the page identifies the
// process without this package knowing anything about it.
//
// html/template rather than string concatenation because VersionInfo is build
// input (ldflags), and build input is still input.
var mainPageTemplate = template.Must(template.New("infra").Parse(`<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <title>{{ .AppName }}</title>
    <meta name="viewport" content="width=device-width, initial-scale=1">
    <style>
        body {
            font-family: system-ui, -apple-system, BlinkMacSystemFont, sans-serif;
            background: #f7f7f8;
            color: #1f2937;
            margin: 0;
            padding: 40px;
        }

        .container {
            max-width: 640px;
            margin: 0 auto;
            background: #ffffff;
            padding: 32px;
            border-radius: 8px;
            box-shadow: 0 10px 20px rgba(0,0,0,0.05);
        }

        h1 {
            margin-top: 0;
            font-size: 24px;
        }

        ul {
            list-style: none;
            padding: 0;
            margin: 24px 0 0;
        }

        li {
            margin-bottom: 12px;
        }

        a {
            display: block;
            padding: 12px 16px;
            background: #f3f4f6;
            border-radius: 6px;
            text-decoration: none;
            color: #111827;
            font-weight: 500;
        }

        a:hover {
            background: #e5e7eb;
        }

        .hint {
            margin-top: 24px;
            font-size: 14px;
            color: #6b7280;
        }

        .meta {
            margin-top: 8px;
            font-size: 13px;
            color: #9ca3af;
        }
    </style>
</head>
<body>
<div class="container">
    <h1>{{ .AppName }}</h1>

    <ul>
        <li><a href="readiness">Readiness</a></li>
        <li><a href="liveness">Liveness</a></li>
        <li><a href="version">Version</a></li>
        <li><a href="metrics">Metrics</a></li>
        {{ if .HasSwagger }}<li><a href="swagger/index.html">Swagger API</a></li>{{ end }}
        <li><a href="debug/pprof/">pprof</a></li>
    </ul>

    <div class="hint">
        Internal service endpoints.
        Intended for operators and developers.
    </div>
    <div class="meta">{{ .Version }} {{ .ShaCommit }}</div>
</div>
</body>
</html>
`))

// mainPage serves an index of the endpoints this server exposes.
func (s *Server) mainPage(c *echo.Context) error {
	page, err := renderMainPage(s.cfg.Version, len(s.cfg.Specs) > 0)
	if err != nil {
		return err
	}

	return c.HTMLBlob(http.StatusOK, page)
}

// mainPageData is what the template renders from.
type mainPageData struct {
	VersionInfo
	HasSwagger bool
}

// renderMainPage builds the index page for the given build metadata.
//
// It renders into a buffer rather than straight to the response so a template
// error cannot surface after a 200 has already been written.
func renderMainPage(v VersionInfo, hasSwagger bool) ([]byte, error) {
	var buf bytes.Buffer
	if err := mainPageTemplate.Execute(&buf, mainPageData{VersionInfo: v, HasSwagger: hasSwagger}); err != nil {
		return nil, fmt.Errorf("render infra main page: %w", err)
	}

	return buf.Bytes(), nil
}
