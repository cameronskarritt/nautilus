package webhook_test

import (
	"strings"
	"testing"

	"nautilus/internal/testutil/require"
	"nautilus/internal/webhook"
)

func TestValidName(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name  string
		input string
		want  bool
	}{
		{"empty", "", false},
		{"name", "Document notifications", true},
		{"maximum runes", strings.Repeat("é", 100), true},
		{"too many runes", strings.Repeat("é", 101), false},
		{"invalid UTF-8", "\xff", false},
		{"control character", "document\nnotifications", false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, webhook.ValidName(tt.input))
		})
	}
}
