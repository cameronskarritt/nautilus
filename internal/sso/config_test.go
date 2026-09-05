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
				"GOOGLE_CLIENT_ID":     "google-client-id",
				"GOOGLE_CLIENT_SECRET": "google-client-secret",
			},
			ExpectedURL: "https://api.example.com",
		},
		{
			Name: "uses google sso base url override",
			Values: map[string]string{
				"API_BASE_URL":         "https://api.example.com",
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
