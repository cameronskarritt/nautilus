package sso

import (
	"context"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"

	"nautilus/internal/errors"
)

const (
	googleProviderName = "google"
	googleIssuer       = "https://accounts.google.com"
)

// GoogleProvider implements the Provider interface for Google OIDC.
type GoogleProvider struct {
	oauth2Config *oauth2.Config
	oidcProvider *oidc.Provider
	verifier     *oidc.IDTokenVerifier
}

// googleClaims represents the claims we extract from Google ID tokens.
type googleClaims struct {
	Email         string `json:"email"`
	EmailVerified bool   `json:"email_verified"`
	Name          string `json:"name"`
	GivenName     string `json:"given_name"`
	FamilyName    string `json:"family_name"`
	Picture       string `json:"picture"`
	HD            string `json:"hd"` // Hosted domain (for Google Workspace)
}

// NewGoogleProvider creates a new Google OIDC provider.
func NewGoogleProvider(ctx context.Context, cfg *GoogleConfig, redirectBaseURL string) (*GoogleProvider, error) {
	// Initialize the OIDC provider (fetches Google's discovery document)
	oidcProvider, err := oidc.NewProvider(ctx, googleIssuer)
	if err != nil {
		return nil, errors.Wrap(err, "failed to create OIDC provider")
	}

	// Configure the OAuth2 client
	oauth2Config := &oauth2.Config{
		ClientID:     cfg.ClientID,
		ClientSecret: cfg.ClientSecret,
		RedirectURL:  redirectBaseURL + "/auth/sso/google/callback",
		Endpoint:     google.Endpoint,
		Scopes:       []string{oidc.ScopeOpenID, "profile", "email"},
	}

	// Create the ID token verifier
	verifier := oidcProvider.Verifier(&oidc.Config{
		ClientID: cfg.ClientID,
	})

	return &GoogleProvider{
		oauth2Config: oauth2Config,
		oidcProvider: oidcProvider,
		verifier:     verifier,
	}, nil
}

// Name returns the provider identifier.
func (p *GoogleProvider) Name() string {
	return googleProviderName
}

// AuthURL returns the Google authorization URL.
func (p *GoogleProvider) AuthURL(state string) string {
	return p.oauth2Config.AuthCodeURL(state, oauth2.AccessTypeOffline)
}

// Exchange exchanges the authorization code for tokens and returns user info.
func (p *GoogleProvider) Exchange(ctx context.Context, code string) (*UserInfo, error) {
	// Exchange code for tokens
	token, err := p.oauth2Config.Exchange(ctx, code)
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
	var claims googleClaims
	if err := idToken.Claims(&claims); err != nil {
		return nil, errors.Wrap(err, "failed to extract claims")
	}

	return &UserInfo{
		ProviderID:    idToken.Subject,
		Email:         claims.Email,
		EmailVerified: claims.EmailVerified,
		Name:          claims.Name,
		GivenName:     claims.GivenName,
		FamilyName:    claims.FamilyName,
	}, nil
}
