package sso

import (
	"context"
	"sync"

	"nautilus/internal/errors"
)

// ErrOrganizationMembership indicates that the provider did not assert the
// active organization membership required by its configuration.
var ErrOrganizationMembership = errors.New("active organization membership required")

// OrganizationInfo identifies an organization asserted by an SSO provider.
type OrganizationInfo struct {
	ProviderID string
	Slug       string
	Name       string
	Admin      bool
}

// UserInfo contains the user information extracted from an OAuth/OIDC provider.
type UserInfo struct {
	ProviderID    string // Unique ID from provider (sub claim for OIDC)
	Email         string
	EmailVerified bool
	Name          string
	GivenName     string
	FamilyName    string
	Organization  *OrganizationInfo
}

// Provider defines the interface for OAuth/OIDC providers.
type Provider interface {
	// Name returns the provider identifier (e.g., "google", "microsoft").
	Name() string

	// AuthURL returns the authorization URL to redirect the user to.
	// The state parameter should be included for CSRF protection.
	AuthURL(state string) string

	// Exchange exchanges an authorization code for user information.
	// For OIDC providers, this verifies the ID token and extracts claims.
	// For OAuth-only providers (e.g., GitHub), this fetches user info from the API.
	Exchange(ctx context.Context, code string) (*UserInfo, error)
}

type Registry struct {
	mu        sync.RWMutex
	providers map[string]Provider
}

func NewRegistry() *Registry {
	return &Registry{
		providers: make(map[string]Provider),
	}
}

func (r *Registry) Register(p Provider) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.providers[p.Name()] = p
}

func (r *Registry) Get(name string) (Provider, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	p, ok := r.providers[name]
	if !ok {
		return nil, errors.Errorf("unknown provider: %s", name)
	}
	return p, nil
}

func (r *Registry) List() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	names := make([]string, 0, len(r.providers))
	for name := range r.providers {
		names = append(names, name)
	}
	return names
}

func (r *Registry) Has(name string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	_, ok := r.providers[name]
	return ok
}
