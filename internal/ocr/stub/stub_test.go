package stub_test

import (
	"context"
	"testing"

	"nautilus/internal/ocr/stub"
	"nautilus/internal/testutil/require"
)

func TestExtract(t *testing.T) {
	t.Parallel()
	text, err := (stub.OCR{}).Extract(t.Context(), nil, "application/pdf")
	require.NoError(t, err)
	require.Empty(t, text)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	text, err = (stub.OCR{}).Extract(ctx, nil, "application/pdf")
	require.ErrorIs(t, err, context.Canceled)
	require.Empty(t, text)
}
