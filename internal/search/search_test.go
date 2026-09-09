package search_test

import (
	"strings"
	"testing"

	"nautilus/internal/search"
	"nautilus/internal/testutil/require"
)

func TestValidID(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name  string
		input string
		want  bool
	}{
		{"empty", "", false},
		{"blank", " \t\n\u2003", false},
		{"opaque ID", "document/1", true},
		{"surrounding whitespace", " document ", true},
		{"maximum bytes", strings.Repeat("é", 256), true},
		{"too many bytes", strings.Repeat("é", 256) + "a", false},
		{"invalid UTF-8", "\xff", false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, search.ValidID(tt.input))
		})
	}
}
