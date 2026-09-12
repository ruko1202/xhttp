package client

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The guard must occupy ControlContext rather than Control.
//
// net.Dialer ignores Control entirely while ControlContext is non-nil —
// measured on go1.25.13, a deny-all Control alongside a permissive
// ControlContext lets the dial through and never runs. A guard installed into
// the weaker field would therefore be disarmed by any later code setting the
// stronger one, with no error, no log, and no failing behavioral test, because
// the connection would simply succeed.
//
// This lives in package client because the choice is invisible from outside:
// both fields are unexported through the public API, so nothing in
// client_test can tell which one was written.
func TestGuardOccupiesTheDominantDialerField(t *testing.T) {
	t.Parallel()

	c := NewClient(WithoutInternalHosts())

	tr, ok := c.Transport.(*transport)
	require.True(t, ok)

	assert.NotNil(t, tr.dialer.ControlContext,
		"the guard belongs in ControlContext, which net.Dialer prefers")
	assert.Nil(t, tr.dialer.Control,
		"Control must stay empty; a guard there is ignored once ControlContext is set")
}
