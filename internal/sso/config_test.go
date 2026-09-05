package sso

import (
	"testing"

	"nautilus/internal/config"
	"nautilus/internal/testutil/require"
)

type mapProvider map[string]string

func (p mapProvider) Get(key string) (string, bool) {
	value, ok := p[key]
	return value, ok
}

func TestLoadConfigGoogleRedirectBaseURL(t *testing.T) {
	tests := []struct {
		Name        string
		Values      map[string]string
		ExpectedURL string
	}{
		{
			Name: "falls back to api base url",
			Values: map[string]string{
				"API_BASE_URL":         "https://api.example.com",
				"SSO_SIGNING_SECRET":   "test-signing-secret",
				"GOOGLE_CLIENT_ID":     "google-client-id",
				"GOOGLE_CLIENT_SECRET": "google-client-secret",
			},
			ExpectedURL: "https://api.example.com",
		},
		{
			Name: "uses google sso base url override",
			Values: map[string]string{
				"API_BASE_URL":         "https://api.example.com",
				"SSO_SIGNING_SECRET":   "test-signing-secret",
				"GOOGLE_SSO_BASE_URL":  "https://smoketest.emulate.dev",
				"GOOGLE_CLIENT_ID":     "google-client-id",
				"GOOGLE_CLIENT_SECRET": "google-client-secret",
			},
			ExpectedURL: "https://smoketest.emulate.dev",
		},
	}

	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			config.SetProvider(mapProvider(tt.Values))
			defer config.SetProvider(new(config.EnvProvider))

			cfg, err := LoadConfig()
			require.NoError(t, err)
			require.NotNil(t, cfg.Google)
			require.Equal(t, tt.ExpectedURL, cfg.Google.RedirectBaseURL)
		})
	}
}

func TestLoadConfigGitHubOrganization(t *testing.T) {
	config.SetProvider(mapProvider{
		"API_BASE_URL":         "https://api.example.com",
		"SSO_SIGNING_SECRET":   "test-signing-secret",
		"GITHUB_CLIENT_ID":     "github-client-id",
		"GITHUB_CLIENT_SECRET": "github-client-secret",
		"GITHUB_ORGANIZATION":  "acme",
	})
	defer config.SetProvider(new(config.EnvProvider))

	cfg, err := LoadConfig()
	require.NoError(t, err)
	require.NotNil(t, cfg.GitHub)
	require.Equal(t, "acme", cfg.GitHub.Organization)
}

func TestLoadConfigRequiresSigningSecret(t *testing.T) {
	tests := []struct {
		name    string
		values  mapProvider
		wantErr bool
	}{
		{name: "no providers", values: mapProvider{}},
		{name: "incomplete provider", values: mapProvider{"GOOGLE_CLIENT_ID": "google-client-id"}},
		{
			name: "google",
			values: mapProvider{
				"GOOGLE_CLIENT_ID":     "google-client-id",
				"GOOGLE_CLIENT_SECRET": "google-client-secret",
			},
			wantErr: true,
		},
		{
			name: "microsoft",
			values: mapProvider{
				"MICROSOFT_CLIENT_ID":     "microsoft-client-id",
				"MICROSOFT_CLIENT_SECRET": "microsoft-client-secret",
			},
			wantErr: true,
		},
		{
			name: "github",
			values: mapProvider{
				"GITHUB_CLIENT_ID":     "github-client-id",
				"GITHUB_CLIENT_SECRET": "github-client-secret",
			},
			wantErr: true,
		},
		{
			name: "apple",
			values: mapProvider{
				"APPLE_CLIENT_ID":   "apple-client-id",
				"APPLE_TEAM_ID":     "apple-team-id",
				"APPLE_KEY_ID":      "apple-key-id",
				"APPLE_PRIVATE_KEY": "apple-private-key",
			},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.values["API_BASE_URL"] = "https://api.example.com"
			config.SetProvider(tt.values)
			t.Cleanup(func() { config.SetProvider(new(config.EnvProvider)) })

			cfg, err := LoadConfig()
			if tt.wantErr {
				require.Error(t, err)
				require.Nil(t, cfg)
				return
			}
			require.NoError(t, err)
			require.NotNil(t, cfg)
			require.Nil(t, cfg.Google)
			require.Nil(t, cfg.Microsoft)
			require.Nil(t, cfg.GitHub)
			require.Nil(t, cfg.Apple)
		})
	}
}
