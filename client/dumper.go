package client

import (
	"bytes"
	"io"
	"net/http"
)

// dumpRequestBody reads the request body for logging and puts it back, so the
// body still reaches the wire in full.
//
// It does not close what it replaces: net/http owns closing the request body.
func dumpRequestBody(req *http.Request) []byte {
	body := dump(req.Body)

	req.Body = io.NopCloser(bytes.NewReader(body))

	return body
}

// dumpResponseBody is the response-side twin, so the caller still reads the
// whole body.
//
// Unlike the request side it must close what it replaces. resp.Body is the
// network body, and the in-memory copy swapped in below is a NopCloser: without
// closing here, the caller's Close would be a no-op on a connection that stays
// open forever.
func dumpResponseBody(resp *http.Response) []byte {
	body := dump(resp.Body)

	if resp.Body != nil {
		_ = resp.Body.Close()
	}
	resp.Body = io.NopCloser(bytes.NewReader(body))

	return body
}

// dump reads a body for logging. It takes an io.Reader rather than an
// io.ReadCloser because it never closes what it is given: closing is a decision
// its two callers make differently.
//
// A read error yields nil rather than propagating: this runs on the logging
// path, which must not change the outcome of the request.
func dump(r io.Reader) []byte {
	if r == nil {
		return nil
	}

	body, err := io.ReadAll(r)
	if err != nil {
		return nil
	}

	return body
}

// truncateForLog bounds a body dump to maxLoggedBodyBytes and marks it when
// cut. The cut is on a byte boundary, so a multi-byte rune straddling the limit
// is split — acceptable for a debug-only dump.
func truncateForLog(body []byte) string {
	if len(body) <= maxLoggedBodyBytes {
		return string(body)
	}

	return string(body[:maxLoggedBodyBytes]) + truncationMarker
}
