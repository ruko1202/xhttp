package sanitize_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ruko1202/xhttp/sanitize"
)

// TestNoopSanitizerReturnsInputUnchanged pins the documented default: installing
// no policy redacts nothing.
func TestNoopSanitizerReturnsInputUnchanged(t *testing.T) {
	t.Parallel()

	s := sanitize.NewNoopSanitizer()

	const rawURL = "https://example.test/path?token=secret"
	require.Equal(t, rawURL, s.SanitizeURL(rawURL))

	headers := http.Header{"Authorization": []string{"Bearer secret"}}
	require.Equal(t, headers, s.SanitizeHeaders(headers))

	body := []byte(`{"password":"secret"}`)
	require.Equal(t, body, s.SanitizeBody(body))
}

// redactor is a minimal policy declared against sanitize.Sanitizer, used to
// check that a third-party implementation satisfies the interface.
type redactor struct{}

func (redactor) SanitizeURL(string) string               { return "[REDACTED]" }
func (redactor) SanitizeHeaders(http.Header) http.Header { return http.Header{} }
func (redactor) SanitizeBody([]byte) []byte              { return []byte("[REDACTED]") }
