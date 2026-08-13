package client

const (
	// maxLoggedBodyBytes bounds the log line, not the wire. A dump longer than
	// this is truncated and marked; the request still goes out in full.
	maxLoggedBodyBytes = 4 << 10 // 4 KiB

	// truncationMarker terminates a body dump cut short by maxLoggedBodyBytes,
	// so a reader can tell a truncated payload from a short one. It is
	// deliberately distinct from anything a sanitizer might use for redaction:
	// "cut for length" and "removed as a secret" must not look alike in a log.
	truncationMarker = "…[TRUNCATED]"

	// bodyLoggingDisabled is logged in place of a body when WithBodyLogging is
	// off, so an empty body and an unlogged one stay distinguishable.
	bodyLoggingDisabled = "body logging disabled"
)
