package infra_test

import (
	"encoding/json"
	"maps"
	"net/http"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ruko1202/xhttp/infra"
)

func TestVersion(t *testing.T) {
	t.Parallel()

	ctx, _ := observedContext(t)
	s := newServer(t, infra.Config{Version: testVersion})

	rec := get(ctx, t, s, "/version")
	require.Equal(t, http.StatusOK, rec.Code)

	var body map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))

	// The key set is a wire contract — consumers parse these names, so a
	// rename is a breaking change rather than a refactor.
	keys := slices.Sorted(maps.Keys(body))

	require.Equal(t, []string{
		"app_name", "arch", "build_time", "os", "sha_commit", "version",
	}, keys)
	require.Equal(t, testVersion.AppName, body["app_name"])
	require.Equal(t, testVersion.ShaCommit, body["sha_commit"])
}
