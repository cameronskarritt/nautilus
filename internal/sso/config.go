package sso

import (
	"context"

	"nautilus/internal/config"
	"nautilus/internal/errors"
)

// Config holds the SSO configuration.
type Config struct {
	RedirectBaseURL string

	Google    *GoogleConfig
	Microsoft *MicrosoftConfig
	GitHub    *GitHubConfig
	Apple     *AppleConfig
}

// GoogleConfig holds Google OAuth configuration.
type GoogleConfig struct {
	ClientID        string
	ClientSecret    string
	RedirectBaseURL string
}

// MicrosoftConfig holds Microsoft/Azure AD OAuth configuration.
type MicrosoftConfig struct {
	ClientID     string
	ClientSecret string
	TenantID     string // Use "common" for multi-tenant, or specific tenant ID
}

// GitHubConfig holds GitHub OAuth configuration.
type GitHubConfig struct {
	ClientID     string
	ClientSecret string
	Organization string
}

// AppleConfig holds Apple Sign In configuration.
type AppleConfig struct {
	ClientID   string // Service ID (e.g., com.example.app)
	TeamID     string // Apple Developer Team ID
	KeyID      string // Key ID for the private key
	PrivateKey string // PEM-encoded private key
}

// LoadConfig loads SSO configuration from environment variables.
func LoadConfig() (*Config, error) {
	cfg := &Config{
		RedirectBaseURL: config.Get[string]("API_BASE_URL"),
	}

	if cfg.RedirectBaseURL == "" {
		return nil, errors.New("API_BASE_URL is required")
	}

	// Load Google config if credentials are present
	googleID := config.Get[string]("GOOGLE_CLIENT_ID")
	googleSecret := config.Get[string]("GOOGLE_CLIENT_SECRET")
	if googleID != "" && googleSecret != "" {
		cfg.Google = &GoogleConfig{
			ClientID:        googleID,
			ClientSecret:    googleSecret,
			RedirectBaseURL: config.Get[string]("GOOGLE_SSO_BASE_URL", cfg.RedirectBaseURL),
		}
	}

	// Load Microsoft config if credentials are present
	msID := config.Get[string]("MICROSOFT_CLIENT_ID")
	msSecret := config.Get[string]("MICROSOFT_CLIENT_SECRET")
	if msID != "" && msSecret != "" {
		cfg.Microsoft = &MicrosoftConfig{
			ClientID:     msID,
			ClientSecret: msSecret,
			TenantID:     config.Get[string]("MICROSOFT_TENANT_ID", "common"),
		}
	}

	// Load GitHub config if credentials are present
	ghID := config.Get[string]("GITHUB_CLIENT_ID")
	ghSecret := config.Get[string]("GITHUB_CLIENT_SECRET")
	if ghID != "" && ghSecret != "" {
		cfg.GitHub = &GitHubConfig{
			ClientID:     ghID,
			ClientSecret: ghSecret,
			Organization: config.Get[string]("GITHUB_ORGANIZATION"),
		}
	}

	// Load Apple config if credentials are present
	appleID := config.Get[string]("APPLE_CLIENT_ID")
	appleTeam := config.Get[string]("APPLE_TEAM_ID")
	appleKey := config.Get[string]("APPLE_KEY_ID")
	applePrivate := config.Get[string]("APPLE_PRIVATE_KEY")
	if appleID != "" && appleTeam != "" && appleKey != "" && applePrivate != "" {
		cfg.Apple = &AppleConfig{
			ClientID:   appleID,
			TeamID:     appleTeam,
			KeyID:      appleKey,
			PrivateKey: applePrivate,
		}
	}
	if (cfg.Google != nil || cfg.Microsoft != nil || cfg.GitHub != nil || cfg.Apple != nil) && config.Get[string]("SSO_SIGNING_SECRET") == "" {
		return nil, errors.New("SSO_SIGNING_SECRET is required when an SSO provider is configured")
	}

	return cfg, nil
}

// SetupRegistry creates a registry with all configured providers.
func SetupRegistry(ctx context.Context, cfg *Config) (*Registry, error) {
	registry := NewRegistry()

	if cfg.Google != nil {
		provider, err := NewGoogleProvider(ctx, cfg.Google, cfg.Google.RedirectBaseURL)
		if err != nil {
			return nil, errors.Wrap(err, "failed to setup Google provider")
		}
		registry.Register(provider)
	}

	if cfg.Microsoft != nil {
		provider, err := NewMicrosoftProvider(ctx, cfg.Microsoft, cfg.RedirectBaseURL)
		if err != nil {
			return nil, errors.Wrap(err, "failed to setup Microsoft provider")
		}
		registry.Register(provider)
	}

	if cfg.GitHub != nil {
		provider := NewGitHubProvider(cfg.GitHub, cfg.RedirectBaseURL)
		registry.Register(provider)
	}

	if cfg.Apple != nil {
		provider, err := NewAppleProvider(ctx, cfg.Apple, cfg.RedirectBaseURL)
		if err != nil {
			return nil, errors.Wrap(err, "failed to setup Apple provider")
		}
		registry.Register(provider)
	}

	return registry, nil
}
