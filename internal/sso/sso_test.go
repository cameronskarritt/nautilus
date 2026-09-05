package sso

import (
	"context"
	"slices"
	"testing"

	"nautilus/internal/testutil/require"
)

type mockProvider struct {
	name string
}

func (m *mockProvider) Name() string {
	return m.name
}

func (m *mockProvider) AuthURL(state string) string {
	return "https://example.com/auth?state=" + state
}

func (m *mockProvider) Exchange(_ context.Context, _ string) (*UserInfo, error) {
	return &UserInfo{
		ProviderID: "mock-123",
		Email:      "test@example.com",
	}, nil
}

func TestRegistry(t *testing.T) {
	t.Parallel()

	registry := NewRegistry()
	google := &mockProvider{name: "google"}
	github := &mockProvider{name: "github"}
	replacementGoogle := &mockProvider{name: "google"}

	registry.Register(google)
	registry.Register(github)
	registry.Register(replacementGoogle)

	provider, err := registry.Get("google")
	require.NoError(t, err)
	require.Equal(t, replacementGoogle, provider)
	require.True(t, registry.Has("github"))
	require.False(t, registry.Has("microsoft"))

	names := registry.List()
	slices.Sort(names)
	require.Equal(t, []string{"github", "google"}, names)
}

func TestRegistry_GetMissingProvider(t *testing.T) {
	t.Parallel()

	registry := NewRegistry()

	_, err := registry.Get("google")
	require.Error(t, err)
	require.Contains(t, err.Error(), "unknown provider: google")
}
