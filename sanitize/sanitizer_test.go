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

// redactor is a policy written the way a service would write one, declared
// outside this package. The assertion below is the point: an implementation
// that never imports anything but net/http satisfies the interface, which is
// what makes this package usable as the seam it claims to be.
type redactor struct{}

func (redactor) SanitizeURL(string) string               { return "[REDACTED]" }
func (redactor) SanitizeHeaders(http.Header) http.Header { return http.Header{} }
func (redactor) SanitizeBody([]byte) []byte              { return []byte("[REDACTED]") }

var _ sanitize.Sanitizer = redactor{}

// TestThirdPartyPolicyIsUsable exercises that implementation through the
// interface, so the compile-time assertion above is backed by a call.
func TestThirdPartyPolicyIsUsable(t *testing.T) {
	t.Parallel()

	var s sanitize.Sanitizer = redactor{}

	require.Equal(t, "[REDACTED]", s.SanitizeURL("https://example.test/?code=live-secret"))
	require.Empty(t, s.SanitizeHeaders(http.Header{"Authorization": []string{"Bearer secret"}}))
	require.Equal(t, []byte("[REDACTED]"), s.SanitizeBody([]byte(`{"password":"secret"}`)))
}
