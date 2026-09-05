package sso

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"testing"

	"golang.org/x/oauth2"

	"nautilus/internal/errors"
	"nautilus/internal/testutil/require"
)

func TestGitHubProviderAuthURL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		Name             string
		Organization     string
		ExpectedOrgScope bool
	}{
		{
			Name:             "user sign in",
			ExpectedOrgScope: false,
		},
		{
			Name:             "organization provisioning",
			Organization:     "acme",
			ExpectedOrgScope: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			t.Parallel()

			provider := NewGitHubProvider(&GitHubConfig{
				ClientID:     "client-id",
				ClientSecret: "client-secret",
				Organization: tt.Organization,
			}, "https://api.example.com")

			authURL, err := url.Parse(provider.AuthURL("state"))
			require.NoError(t, err)
			scopes := strings.Fields(authURL.Query().Get("scope"))
			require.Equal(t, tt.ExpectedOrgScope, slices.Contains(scopes, "read:org"))
		})
	}
}

func TestGitHubProviderExchangeWithOrganization(t *testing.T) {
	t.Parallel()

	server := newGitHubServer(t, http.StatusOK, "active", "admin")
	provider := newTestGitHubProvider(server.URL)

	user, err := provider.Exchange(context.Background(), "code")
	require.NoError(t, err)
	require.Equal(t, "42", user.ProviderID)
	require.Equal(t, "mona@example.com", user.Email)
	require.True(t, user.EmailVerified)
	require.NotNil(t, user.Organization)
	require.Equal(t, "99", user.Organization.ProviderID)
	require.Equal(t, "acme", user.Organization.Slug)
	require.True(t, user.Organization.Admin)
}

func TestGitHubProviderExchangeRequiresActiveOrganizationMembership(t *testing.T) {
	t.Parallel()

	tests := []struct {
		Name       string
		StatusCode int
		State      string
	}{
		{
			Name:       "not a member",
			StatusCode: http.StatusNotFound,
		},
		{
			Name:       "pending member",
			StatusCode: http.StatusOK,
			State:      "pending",
		},
		{
			Name:       "organization blocks access",
			StatusCode: http.StatusForbidden,
		},
	}

	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			t.Parallel()

			server := newGitHubServer(t, tt.StatusCode, tt.State, "member")
			provider := newTestGitHubProvider(server.URL)

			_, err := provider.Exchange(context.Background(), "code")
			require.Error(t, err)
			require.True(t, errors.Is(err, ErrOrganizationMembership))
		})
	}
}

func newTestGitHubProvider(baseURL string) *GitHubProvider {
	provider := NewGitHubProvider(&GitHubConfig{
		ClientID:     "client-id",
		ClientSecret: "client-secret",
		Organization: "acme",
	}, "https://api.example.com")
	provider.oauth2Config.Endpoint = oauth2.Endpoint{
		AuthURL:  baseURL + "/login/oauth/authorize",
		TokenURL: baseURL + "/login/oauth/access_token",
	}
	provider.apiBaseURL = baseURL
	provider.userAPI = baseURL + "/user"
	provider.emailsAPI = baseURL + "/user/emails"
	return provider
}

func newGitHubServer(t *testing.T, membershipStatus int, state string, role string) *httptest.Server {
	t.Helper()

	mux := http.NewServeMux()
	mux.HandleFunc("POST /login/oauth/access_token", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"token","token_type":"bearer"}`))
	})
	mux.HandleFunc("GET /user", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"id":42,"login":"mona","name":"Mona Lisa"}`))
	})
	mux.HandleFunc("GET /user/emails", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`[{"email":"mona@example.com","primary":true,"verified":true}]`))
	})
	mux.HandleFunc("GET /user/memberships/orgs/acme", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(membershipStatus)
		if membershipStatus == http.StatusOK {
			_, _ = w.Write([]byte(`{"state":"` + state + `","role":"` + role + `","organization":{"id":99,"login":"acme"}}`))
		}
	})

	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return server
}
