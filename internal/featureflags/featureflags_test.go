package featureflags

import (
	"testing"

	"nautilus/internal/testutil/require"
)

func TestMergeFlags(t *testing.T) {
	t.Parallel()

	tests := []struct {
		Name      string
		OrgFlags  map[string]bool
		UserFlags map[string]bool
		Expected  map[string]bool
	}{
		{
			Name: "org flags only",
			OrgFlags: map[string]bool{
				"alpha": true,
			},
			Expected: map[string]bool{
				"alpha": true,
			},
		},
		{
			Name: "user flags override org flags",
			OrgFlags: map[string]bool{
				"alpha": true,
				"beta":  false,
			},
			UserFlags: map[string]bool{
				"beta":  true,
				"gamma": true,
			},
			Expected: map[string]bool{
				"alpha": true,
				"beta":  true,
				"gamma": true,
			},
		},
		{
			Name:     "nil inputs",
			Expected: map[string]bool{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, tt.Expected, MergeFlags(tt.OrgFlags, tt.UserFlags))
		})
	}
}
