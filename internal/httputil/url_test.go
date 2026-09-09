package httputil_test

import (
	"testing"

	"nautilus/internal/httputil"
	"nautilus/internal/testutil/require"
)

func TestParseBaseURL(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name, raw string
		valid     bool
	}{
		{name: "local HTTP", raw: "http://localhost:1234/v1", valid: true},
		{name: "HTTPS path", raw: "https://model.example/proxy/v1/", valid: true},
		{name: "IPv6", raw: "http://[::1]:1234/v1", valid: true},
		{name: "encoded path", raw: "https://model.example/a%2Fb", valid: true},
		{name: "empty"},
		{name: "relative path", raw: "/v1"},
		{name: "missing host", raw: "http:///v1"},
		{name: "unsupported scheme", raw: "file:///v1"},
		{name: "malformed escape", raw: "http://model.example/%"},
		{name: "credentials", raw: "https://user:secret@model.example/v1"},
		{name: "empty credentials", raw: "https://@model.example/v1"},
		{name: "query", raw: "https://model.example/v1?key=secret"},
		{name: "empty query", raw: "https://model.example/v1?"},
		{name: "fragment", raw: "https://model.example/v1#secret"},
		{name: "empty fragment", raw: "https://model.example/v1#"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			u := httputil.ParseBaseURL(tt.raw)
			if !tt.valid {
				require.Nil(t, u)
				return
			}
			require.NotNil(t, u)
			require.Equal(t, tt.raw, u.String())
		})
	}
}
