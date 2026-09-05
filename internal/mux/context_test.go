package mux

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"nautilus/internal/testutil/require"
)

func TestPathParam(t *testing.T) {
	t.Parallel()

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req = setPathParams(req, map[string]string{
		"user": "123",
		"page": "home",
	})

	value, ok := PathParam(req, "user")
	require.True(t, ok)
	require.Equal(t, "123", value)

	value, ok = PathParam(req, "missing")
	require.False(t, ok)
	require.Equal(t, "", value)
}

func TestPathParamWithoutParams(t *testing.T) {
	t.Parallel()

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	value, ok := PathParam(req, "user")

	require.False(t, ok)
	require.Equal(t, "", value)
}
