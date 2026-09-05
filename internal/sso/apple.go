package sso

import (
	"context"
	"crypto/ecdsa"
	"crypto/x509"
	"encoding/pem"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"

	"nautilus/internal/errors"
)

const (
	appleProviderName = "apple"
	appleIssuer       = "https://appleid.apple.com"
	appleAuthURL      = "https://appleid.apple.com/auth/authorize"
	appleTokenURL     = "https://appleid.apple.com/auth/token"
)

// AppleProvider implements the Provider interface for Apple Sign In.
// Apple uses OIDC but requires a JWT client secret generated from a private key.
type AppleProvider struct {
	oauth2Config *oauth2.Config
	oidcProvider *oidc.Provider
	verifier     *oidc.IDTokenVerifier

	// For generating client secrets
	teamID     string
	keyID      string
	privateKey *ecdsa.PrivateKey
	clientID   string
}

// appleClaims represents the claims we extract from Apple ID tokens.
// Note: Apple only sends email and name on the first authorization.
type appleClaims struct {
	Email         string `json:"email"`
	EmailVerified any    `json:"email_verified"` // Can be bool or string
}

// NewAppleProvider creates a new Apple Sign In provider.
func NewAppleProvider(ctx context.Context, cfg *AppleConfig, redirectBaseURL string) (*AppleProvider, error) {
	// Parse the private key
	privateKey, err := parseApplePrivateKey(cfg.PrivateKey)
	if err != nil {
		return nil, errors.Wrap(err, "failed to parse private key")
	}

	// Initialize the OIDC provider
	oidcProvider, err := oidc.NewProvider(ctx, appleIssuer)
	if err != nil {
		return nil, errors.Wrap(err, "failed to create OIDC provider")
	}

	provider := &AppleProvider{
		oidcProvider: oidcProvider,
		teamID:       cfg.TeamID,
		keyID:        cfg.KeyID,
		privateKey:   privateKey,
		clientID:     cfg.ClientID,
	}

	// Configure the OAuth2 client
	// We'll set the client secret dynamically in Exchange()
	provider.oauth2Config = &oauth2.Config{
		ClientID:    cfg.ClientID,
		RedirectURL: redirectBaseURL + "/auth/sso/apple/callback",
		Endpoint: oauth2.Endpoint{
			AuthURL:  appleAuthURL,
			TokenURL: appleTokenURL,
		},
		Scopes: []string{"name", "email"},
	}

	// Create the ID token verifier
	provider.verifier = oidcProvider.Verifier(&oidc.Config{
		ClientID: cfg.ClientID,
	})

	return provider, nil
}

// Name returns the provider identifier.
func (p *AppleProvider) Name() string {
	return appleProviderName
}

// AuthURL returns the Apple authorization URL.
func (p *AppleProvider) AuthURL(state string) string {
	// Apple requires response_mode=form_post for web apps
	return p.oauth2Config.AuthCodeURL(state,
		oauth2.SetAuthURLParam("response_mode", "form_post"),
	)
}

// Exchange exchanges the authorization code for tokens and returns user info.
func (p *AppleProvider) Exchange(ctx context.Context, code string) (*UserInfo, error) {
	// Generate a fresh client secret (Apple requires JWT client secrets)
	clientSecret, err := p.generateClientSecret()
	if err != nil {
		return nil, errors.Wrap(err, "failed to generate client secret")
	}

	// Create a copy of the config with the client secret
	config := *p.oauth2Config
	config.ClientSecret = clientSecret

	// Exchange code for tokens
	token, err := config.Exchange(ctx, code)
	if err != nil {
		return nil, errors.Wrap(err, "failed to exchange code")
	}

	// Extract ID token from the OAuth2 token
	rawIDToken, ok := token.Extra("id_token").(string)
	if !ok {
		return nil, errors.New("no id_token in token response")
	}

	// Verify the ID token
	idToken, err := p.verifier.Verify(ctx, rawIDToken)
	if err != nil {
		return nil, errors.Wrap(err, "failed to verify ID token")
	}

	// Extract claims
	var claims appleClaims
	if err := idToken.Claims(&claims); err != nil {
		return nil, errors.Wrap(err, "failed to extract claims")
	}

	// Handle email_verified being a string or bool
	emailVerified := false
	switch v := claims.EmailVerified.(type) {
	case bool:
		emailVerified = v
	case string:
		emailVerified = v == "true"
	}

	return &UserInfo{
		ProviderID:    idToken.Subject,
		Email:         claims.Email,
		EmailVerified: emailVerified,
		// Note: Apple only sends name on first authorization via form_post
		// We don't have access to it here in the code exchange
		Name:       "",
		GivenName:  "",
		FamilyName: "",
	}, nil
}

// generateClientSecret creates a JWT client secret for Apple Sign In.
func (p *AppleProvider) generateClientSecret() (string, error) {
	return signAppleClientSecret(p.teamID, p.clientID, p.keyID, p.privateKey)
}

// parseApplePrivateKey parses a PEM-encoded ECDSA private key.
func parseApplePrivateKey(pemKey string) (*ecdsa.PrivateKey, error) {
	block, _ := pem.Decode([]byte(pemKey))
	if block == nil {
		return nil, errors.New("failed to decode PEM block")
	}

	// Try parsing as PKCS8 first (most common format from Apple)
	key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err == nil {
		ecKey, ok := key.(*ecdsa.PrivateKey)
		if !ok {
			return nil, errors.New("key is not an ECDSA private key")
		}
		return ecKey, nil
	}

	// Fall back to EC private key format
	ecKey, err := x509.ParseECPrivateKey(block.Bytes)
	if err != nil {
		return nil, errors.Wrap(err, "failed to parse private key")
	}

	return ecKey, nil
}
