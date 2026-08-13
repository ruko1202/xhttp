package lifecycle_test

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ruko1202/xhttp/lifecycle"
)

func TestDrainer(t *testing.T) {
	t.Parallel()

	t.Run("starts not draining", func(t *testing.T) {
		t.Parallel()
		d := lifecycle.NewDrainer()
		require.False(t, d.IsDraining())
	})

	t.Run("StartDraining flips the flag", func(t *testing.T) {
		t.Parallel()
		d := lifecycle.NewDrainer()
		d.StartDraining()
		require.True(t, d.IsDraining())
	})

	t.Run("StartDraining is idempotent", func(t *testing.T) {
		t.Parallel()
		d := lifecycle.NewDrainer()
		d.StartDraining()
		d.StartDraining()
		require.True(t, d.IsDraining())
	})

	t.Run("satisfies DrainChecker", func(t *testing.T) {
		t.Parallel()
		var c lifecycle.DrainChecker = lifecycle.NewDrainer()
		require.False(t, c.IsDraining())
	})

	t.Run("concurrent access is race-free", func(t *testing.T) {
		t.Parallel()
		d := lifecycle.NewDrainer()
		var wg sync.WaitGroup
		for range 50 {
			wg.Add(2)
			go func() { defer wg.Done(); d.StartDraining() }()
			go func() { defer wg.Done(); _ = d.IsDraining() }()
		}
		wg.Wait()
		require.True(t, d.IsDraining())
	})
}
