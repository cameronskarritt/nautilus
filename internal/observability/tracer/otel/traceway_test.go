package otel

import (
	"testing"

	"nautilus/internal/testutil/require"
)

func TestTracewayTraceEndpoint(t *testing.T) {
	t.Parallel()

	tests := []struct {
		Name     string
		Endpoint string
		Expected string
	}{
		{
			Name:     "empty endpoint",
			Endpoint: "",
			Expected: "",
		},
		{
			Name:     "base otel endpoint",
			Endpoint: "https://traceway.example.com/api/otel",
			Expected: "https://traceway.example.com/api/otel/v1/traces",
		},
		{
			Name:     "base otel endpoint with trailing slash",
			Endpoint: "https://traceway.example.com/api/otel/",
			Expected: "https://traceway.example.com/api/otel/v1/traces",
		},
		{
			Name:     "trace endpoint",
			Endpoint: "https://traceway.example.com/api/otel/v1/traces",
			Expected: "https://traceway.example.com/api/otel/v1/traces",
		},
	}

	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, tt.Expected, TracewayTraceEndpoint(tt.Endpoint))
		})
	}
}
