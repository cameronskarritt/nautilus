package sso

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/github"

	"nautilus/internal/errors"
)

const (
	githubProviderName = "github"
	githubAPIBaseURL   = "https://api.github.com"
	githubUserAPI      = "https://api.github.com/user"
	githubEmailsAPI    = "https://api.github.com/user/emails"
)

// GitHubProvider implements the Provider interface for GitHub OAuth.
// Note: GitHub uses OAuth2 only, not OIDC, so we need to fetch user info via API.
type GitHubProvider struct {
	oauth2Config *oauth2.Config
	apiBaseURL   string
	userAPI      string
	emailsAPI    string
	organization string
}

// githubUser represents the response from GitHub's /user endpoint.
type githubUser struct {
	ID        int64  `json:"id"`
	Login     string `json:"login"`
	Name      string `json:"name"`
	Email     string `json:"email"`
	AvatarURL string `json:"avatar_url"`
}

// githubEmail represents an email from GitHub's /user/emails endpoint.
type githubEmail struct {
	Email    string `json:"email"`
	Primary  bool   `json:"primary"`
	Verified bool   `json:"verified"`
}

type githubOrganizationMembership struct {
	State        string `json:"state"`
	Role         string `json:"role"`
	Organization struct {
		ID    int64  `json:"id"`
		Login string `json:"login"`
	} `json:"organization"`
}

// NewGitHubProvider creates a new GitHub OAuth provider.
func NewGitHubProvider(cfg *GitHubConfig, redirectBaseURL string) *GitHubProvider {
	organization := strings.TrimSpace(cfg.Organization)
	scopes := []string{"read:user", "user:email"}
	if organization != "" {
		scopes = append(scopes, "read:org")
	}

	oauth2Config := &oauth2.Config{
		ClientID:     cfg.ClientID,
		ClientSecret: cfg.ClientSecret,
		RedirectURL:  redirectBaseURL + "/auth/sso/github/callback",
		Endpoint:     github.Endpoint,
		Scopes:       scopes,
	}

	return &GitHubProvider{
		oauth2Config: oauth2Config,
		apiBaseURL:   githubAPIBaseURL,
		userAPI:      githubUserAPI,
		emailsAPI:    githubEmailsAPI,
		organization: organization,
	}
}

// Name returns the provider identifier.
func (p *GitHubProvider) Name() string {
	return githubProviderName
}

// AuthURL returns the GitHub authorization URL.
func (p *GitHubProvider) AuthURL(state string) string {
	return p.oauth2Config.AuthCodeURL(state)
}

// Exchange exchanges the authorization code for tokens and returns user info.
func (p *GitHubProvider) Exchange(ctx context.Context, code string) (*UserInfo, error) {
	// Exchange code for token
	token, err := p.oauth2Config.Exchange(ctx, code)
	if err != nil {
		return nil, errors.Wrap(err, "failed to exchange code")
	}

	// Create HTTP client with the token
	client := p.oauth2Config.Client(ctx, token)

	// Fetch user info
	user, err := p.fetchUser(client)
	if err != nil {
		return nil, err
	}

	// Fetch primary verified email
	email, emailVerified, err := p.fetchPrimaryEmail(client)
	if err != nil {
		// Fall back to the email from user profile if available
		email = user.Email
		emailVerified = false
	}

	var organization *OrganizationInfo
	if p.organization != "" {
		membership, err := p.fetchOrganizationMembership(client)
		if err != nil {
			return nil, err
		}
		if membership.State != "active" {
			return nil, ErrOrganizationMembership
		}
		if membership.Organization.ID <= 0 ||
			!strings.EqualFold(membership.Organization.Login, p.organization) {
			return nil, errors.New("invalid GitHub organization membership response")
		}
		organization = &OrganizationInfo{
			ProviderID: strconv.FormatInt(membership.Organization.ID, 10),
			Slug:       membership.Organization.Login,
			Name:       membership.Organization.Login,
			Admin:      membership.Role == "admin",
		}
	}

	// Parse name into given/family name (best effort)
	givenName, familyName := parseName(user.Name)

	return &UserInfo{
		ProviderID:    strconv.FormatInt(user.ID, 10),
		Email:         email,
		EmailVerified: emailVerified,
		Name:          user.Name,
		GivenName:     givenName,
		FamilyName:    familyName,
		Organization:  organization,
	}, nil
}

func (p *GitHubProvider) fetchUser(client *http.Client) (*githubUser, error) {
	resp, err := client.Get(p.userAPI)
	if err != nil {
		return nil, errors.Wrap(err, "failed to fetch user")
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, errors.Errorf("unexpected status from GitHub API: %d", resp.StatusCode)
	}

	var user githubUser
	if err := json.NewDecoder(resp.Body).Decode(&user); err != nil {
		return nil, errors.Wrap(err, "failed to decode user response")
	}

	return &user, nil
}

func (p *GitHubProvider) fetchPrimaryEmail(client *http.Client) (string, bool, error) {
	resp, err := client.Get(p.emailsAPI)
	if err != nil {
		return "", false, errors.Wrap(err, "failed to fetch emails")
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", false, errors.Errorf("unexpected status from GitHub emails API: %d", resp.StatusCode)
	}

	var emails []githubEmail
	if err := json.NewDecoder(resp.Body).Decode(&emails); err != nil {
		return "", false, errors.Wrap(err, "failed to decode emails response")
	}

	// Find primary verified email
	for _, e := range emails {
		if e.Primary && e.Verified {
			return e.Email, true, nil
		}
	}

	// Fall back to any verified email
	for _, e := range emails {
		if e.Verified {
			return e.Email, true, nil
		}
	}

	// Fall back to primary email even if not verified
	for _, e := range emails {
		if e.Primary {
			return e.Email, false, nil
		}
	}

	return "", false, errors.New("no email found")
}

func (p *GitHubProvider) fetchOrganizationMembership(client *http.Client) (*githubOrganizationMembership, error) {
	endpoint := p.apiBaseURL + "/user/memberships/orgs/" + url.PathEscape(p.organization)
	resp, err := client.Get(endpoint)
	if err != nil {
		return nil, errors.Wrap(err, "failed to fetch organization membership")
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusNotFound {
		return nil, ErrOrganizationMembership
	}
	if resp.StatusCode != http.StatusOK {
		return nil, errors.Errorf("unexpected status from GitHub organization membership API: %d", resp.StatusCode)
	}

	var membership githubOrganizationMembership
	if err := json.NewDecoder(resp.Body).Decode(&membership); err != nil {
		return nil, errors.Wrap(err, "failed to decode organization membership response")
	}

	return &membership, nil
}

// parseName attempts to split a full name into given and family names.
func parseName(name string) (givenName, familyName string) {
	if name == "" {
		return "", ""
	}

	// Simple split on first space
	for i, c := range name {
		if c == ' ' {
			return name[:i], name[i+1:]
		}
	}

	// No space found, assume it's all given name
	return name, ""
}
