package temporal_test

import (
	"testing"

	"nautilus/internal/temporal"
	"nautilus/internal/testutil/require"
)

func TestRunWorkersRequiresQueue(t *testing.T) {
	t.Parallel()
	require.Error(t, temporal.RunWorkers(t.Context(), nil))
}
