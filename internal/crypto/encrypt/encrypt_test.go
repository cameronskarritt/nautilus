package encrypt

import (
	"testing"

	"nautilus/internal/testutil/require"
)

func TestContextHelpers(t *testing.T) {
	t.Parallel()
	enc := ForUser(&keyManager{})
	ctx := WithContext(t.Context(), enc)
	require.Equal(t, enc, FromContext(ctx))
	require.Nil(t, FromContext(t.Context()))
}
