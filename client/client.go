// Package client provides an *http.Client with structured logging and tracing
// built in.
//
// Every exchange is logged through xlog and wrapped in a span, so outgoing
// calls show up in traces alongside the operation that made them. What may be
// written to those logs is the caller's decision: see sanitize.Sanitizer.
//
// The zero-configuration client is not redaction-safe. NewClient() with no
// options logs URLs and headers verbatim, so any service whose requests carry
// credentials should pass WithSanitizer.
package client

import (
	"net/http"
	"time"
)

// defaultTimeout bounds a whole request: connection, sending, and reading the
// response body to completion. 30s is a safe ceiling rather than a good target
// — APIs expected to answer quickly deserve a tighter WithTimeout.
const defaultTimeout = 30 * time.Second

// Option configures the client returned by NewClient.
type Option func(*http.Client)

// NewClient returns an *http.Client that logs and traces every exchange.
//
// The returned client is an ordinary *http.Client: it can be handed to any
// library that accepts one, and it is safe for concurrent use.
func NewClient(opts ...Option) *http.Client {
	client := &http.Client{
		Timeout:   defaultTimeout,
		Transport: newTransport(),
	}

	for _, opt := range opts {
		opt(client)
	}

	return client
}
