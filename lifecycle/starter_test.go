package lifecycle_test

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ruko1202/xhttp/lifecycle"
)

func TestStarter(t *testing.T) {
	t.Parallel()

	t.Run("starts not ready", func(t *testing.T) {
		t.Parallel()
		s := lifecycle.NewStarter()
		require.False(t, s.IsReady())
	})

	// A replica must opt in to traffic: a Starter embedded in a struct and
	// never constructed must not report ready by accident.
	t.Run("zero value is not ready", func(t *testing.T) {
		t.Parallel()
		var s lifecycle.Starter
		require.False(t, s.IsReady())
	})

	t.Run("MarkReady flips the flag", func(t *testing.T) {
		t.Parallel()
		s := lifecycle.NewStarter()
		s.MarkReady()
		require.True(t, s.IsReady())
	})

	t.Run("MarkReady is idempotent", func(t *testing.T) {
		t.Parallel()
		s := lifecycle.NewStarter()
		s.MarkReady()
		s.MarkReady()
		require.True(t, s.IsReady())
	})

	t.Run("satisfies ReadyChecker", func(t *testing.T) {
		t.Parallel()
		var c lifecycle.ReadyChecker = lifecycle.NewStarter()
		require.False(t, c.IsReady())
	})

	t.Run("concurrent access is race-free", func(t *testing.T) {
		t.Parallel()
		s := lifecycle.NewStarter()
		var wg sync.WaitGroup
		for range 50 {
			wg.Add(2)
			go func() { defer wg.Done(); s.MarkReady() }()
			go func() { defer wg.Done(); _ = s.IsReady() }()
		}
		wg.Wait()
		require.True(t, s.IsReady())
	})
}
