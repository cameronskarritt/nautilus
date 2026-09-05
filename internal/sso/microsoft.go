package sso

import (
	"context"
	"fmt"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/microsoft"

	"nautilus/internal/errors"
)

const (
	microsoftProviderName = "microsoft"
)

// microsoftIssuer returns the issuer URL for the given tenant.
// Use "common" for multi-tenant apps, "organizations" for work/school accounts only,
// "consumers" for personal accounts only, or a specific tenant ID.
func microsoftIssuer(tenantID string) string {
	return fmt.Sprintf("https://login.microsoftonline.com/%s/v2.0", tenantID)
}

// MicrosoftProvider implements the Provider interface for Microsoft/Azure AD OIDC.
type MicrosoftProvider struct {
	oauth2Config *oauth2.Config
	oidcProvider *oidc.Provider
	verifier     *oidc.IDTokenVerifier
}

// microsoftClaims represents the claims we extract from Microsoft ID tokens.
type microsoftClaims struct {
	Email             string `json:"email"`
	PreferredUsername string `json:"preferred_username"`
	Name              string `json:"name"`
	GivenName         string `json:"given_name"`
	FamilyName        string `json:"family_name"`
}

// NewMicrosoftProvider creates a new Microsoft OIDC provider.
func NewMicrosoftProvider(ctx context.Context, cfg *MicrosoftConfig, redirectBaseURL string) (*MicrosoftProvider, error) {
	tenantID := cfg.TenantID
	if tenantID == "" {
		tenantID = "common"
	}

	// Initialize the OIDC provider
	oidcProvider, err := oidc.NewProvider(ctx, microsoftIssuer(tenantID))
	if err != nil {
		return nil, errors.Wrap(err, "failed to create OIDC provider")
	}

	// Configure the OAuth2 client
	oauth2Config := &oauth2.Config{
		ClientID:     cfg.ClientID,
		ClientSecret: cfg.ClientSecret,
		RedirectURL:  redirectBaseURL + "/auth/sso/microsoft/callback",
		Endpoint:     microsoft.AzureADEndpoint(tenantID),
		Scopes:       []string{oidc.ScopeOpenID, "profile", "email"},
	}

	// Create the ID token verifier
	// For multi-tenant apps, we need to skip issuer validation since it varies by tenant
	verifierConfig := &oidc.Config{
		ClientID: cfg.ClientID,
	}
	if tenantID == "common" || tenantID == "organizations" || tenantID == "consumers" {
		verifierConfig.SkipIssuerCheck = true
	}
	verifier := oidcProvider.Verifier(verifierConfig)

	return &MicrosoftProvider{
		oauth2Config: oauth2Config,
		oidcProvider: oidcProvider,
		verifier:     verifier,
	}, nil
}

// Name returns the provider identifier.
func (p *MicrosoftProvider) Name() string {
	return microsoftProviderName
}

// AuthURL returns the Microsoft authorization URL.
func (p *MicrosoftProvider) AuthURL(state string) string {
	return p.oauth2Config.AuthCodeURL(state, oauth2.AccessTypeOffline)
}

// Exchange exchanges the authorization code for tokens and returns user info.
func (p *MicrosoftProvider) Exchange(ctx context.Context, code string) (*UserInfo, error) {
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
	var claims microsoftClaims
	if err := idToken.Claims(&claims); err != nil {
		return nil, errors.Wrap(err, "failed to extract claims")
	}

	// Microsoft may not always return email in the email claim
	// Fall back to preferred_username if needed
	email := claims.Email
	if email == "" {
		email = claims.PreferredUsername
	}

	return &UserInfo{
		ProviderID:    idToken.Subject,
		Email:         email,
		EmailVerified: true, // Microsoft accounts are always verified
		Name:          claims.Name,
		GivenName:     claims.GivenName,
		FamilyName:    claims.FamilyName,
	}, nil
}
