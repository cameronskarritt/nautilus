package auth

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"nautilus/internal/config"
	"nautilus/internal/database"
	"nautilus/internal/database/organizations"
	"nautilus/internal/database/orgidentities"
	"nautilus/internal/database/sessions"
	"nautilus/internal/database/users"
	"nautilus/internal/enums"
	"nautilus/internal/errors"
	"nautilus/internal/log"
	"nautilus/internal/mail"
	"nautilus/internal/mux"
	"nautilus/internal/optional"
	"nautilus/internal/sso"
)

var errSSOEmailExists = errors.New("SSO email already exists")

type provisionedSSOUser struct {
	user    *users.User
	member  *organizations.Member
	created bool
}

type SSOMux struct {
	db       database.Database
	sender   mail.Sender
	registry *sso.Registry
}

func NewSSOMux(ctx context.Context, db database.Database, sender mail.Sender) *SSOMux {
	logger := log.FromContext(ctx)

	cfg, err := sso.LoadConfig()
	if err != nil {
		logger.Info("SSO not configured", "error", err)
		return &SSOMux{db: db, sender: sender}
	}

	registry, err := sso.SetupRegistry(ctx, cfg)
	if err != nil {
		logger.Error("failed to setup SSO registry", "error", err)
		return &SSOMux{db: db, sender: sender}
	}

	logger.Info("SSO registry setup", "providers", registry.List())

	return &SSOMux{
		db:       db,
		sender:   sender,
		registry: registry,
	}
}

func (s *SSOMux) Enabled() bool {
	return s.registry != nil
}

func (s *SSOMux) Providers() []string {
	if s.registry == nil {
		return []string{}
	}
	return s.registry.List()
}

func (s *SSOMux) Mount(r *mux.Router, prefix string) {
	sub := r.SubRouter(prefix)

	sub.Get("/{provider}", s.Start)
	sub.Get("/{provider}/callback", s.Callback)
	// Apple uses form_post, so we need to handle POST as well
	sub.Post("/{provider}/callback", s.Callback)
}

// Start initiates the OAuth flow for the specified provider.
func (s *SSOMux) Start(w http.ResponseWriter, r *http.Request) {
	logger := log.FromContext(r.Context())

	providerName, _ := mux.PathParam(r, "provider")
	logger.Info("SSO provider requested", "provider", providerName)
	if providerName == "" || s.registry == nil {
		logger.Warn("unknown SSO provider requested", "provider", providerName)
		s.errorRedirect(w, r, ErrInvalidProvider)
		return
	}

	provider, err := s.registry.Get(providerName)
	if err != nil {
		logger.Warn("unknown SSO provider requested", "provider", providerName)
		s.errorRedirect(w, r, ErrInvalidProvider)
		return
	}

	// Get optional redirect URL from query params
	redirectURL := r.URL.Query().Get("redirect")

	// Generate state token
	secret := config.Get[string]("SSO_SIGNING_SECRET")
	state, err := sso.GenerateState(w, secret, providerName, redirectURL)
	if err != nil {
		logger.Error("failed to generate SSO state", "error", err)
		s.errorRedirect(w, r, ErrSSOServerError)
		return
	}

	// Redirect to provider
	authURL := provider.AuthURL(state)
	http.Redirect(w, r, authURL, http.StatusFound)
}

// Callback handles the OAuth callback from the provider.
func (s *SSOMux) Callback(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	logger := log.FromContext(ctx)

	providerName, ok := mux.PathParam(r, "provider")
	if providerName == "" || !ok {
		s.errorRedirect(w, r, ErrInvalidProvider)
		return
	}
	// Handle errors from the provider
	if errParam := r.URL.Query().Get("error"); errParam != "" {
		errDesc := r.URL.Query().Get("error_description")
		logger.Warn("SSO provider returned error", "provider", providerName, "error", errParam, "description", errDesc)
		sso.ClearState(w)
		s.errorRedirect(w, r, ErrSSOProviderError)
		return
	}

	// Get the authorization code
	code := r.URL.Query().Get("code")
	if code == "" {
		// Apple uses form_post, so check POST body as well
		if r.Method == http.MethodPost {
			if err := r.ParseForm(); err == nil {
				code = r.FormValue("code")
			}
		}
	}
	if code == "" {
		logger.Warn("SSO callback missing authorization code", "provider", providerName)
		sso.ClearState(w)
		s.errorRedirect(w, r, ErrSSOMissingCode)
		return
	}

	// Get and verify state
	state := r.URL.Query().Get("state")
	if state == "" && r.Method == http.MethodPost {
		state = r.FormValue("state")
	}

	secret := config.Get[string]("SSO_SIGNING_SECRET")
	stateResult, err := sso.VerifyState(r, secret, state)
	if err != nil {
		logger.Warn("SSO state verification failed", "provider", providerName, "error", err)
		sso.ClearState(w)
		s.errorRedirect(w, r, ErrSSOInvalidState)
		return
	}

	// Verify provider matches
	if stateResult.Provider != providerName {
		logger.Warn("SSO provider mismatch", "expected", stateResult.Provider, "got", providerName)
		sso.ClearState(w)
		s.errorRedirect(w, r, ErrSSOProviderMismatch)
		return
	}

	// Clear the state cookie
	sso.ClearState(w)

	// Get the provider
	provider, err := s.registry.Get(providerName)
	if err != nil {
		logger.Error("failed to get SSO provider", "provider", providerName, "error", err)
		s.errorRedirect(w, r, ErrInvalidProvider)
		return
	}

	// Exchange code for user info
	userInfo, err := provider.Exchange(ctx, code)
	if err != nil {
		logger.Error("failed to exchange SSO code", "provider", providerName, "error", err)
		if errors.Is(err, sso.ErrOrganizationMembership) {
			s.errorRedirect(w, r, ErrSSOOrganizationMembership)
			return
		}
		s.errorRedirect(w, r, ErrSSOExchangeFailed)
		return
	}

	// Map provider name to auth provider enum
	authProvider := providerToAuthProvider(providerName)

	provisioned, err := s.provision(ctx, authProvider, userInfo)
	if err != nil {
		if errors.Is(err, errSSOEmailExists) {
			logger.Info("SSO login with email already registered", "email", userInfo.Email, "provider", providerName)
			s.errorRedirect(w, r, ErrSSOEmailExists)
			return
		}
		logger.Error("failed to provision SSO user", "provider", providerName, "error", err)
		s.errorRedirect(w, r, ErrSSOServerError)
		return
	}

	if provisioned.created && provisioned.user.Email.Set {
		mailErr := mail.SendTemplate(ctx, s.sender, provisioned.user.Email.Data, enums.MailTemplateWelcome, nil)
		if mailErr != nil {
			logger.Error("error sending welcome email from SSO login", "error", mailErr)
		}
	}

	var orgMemberID optional.Optional[int]
	if provisioned.member != nil {
		orgMemberID = optional.Set(provisioned.member.ID)
	}

	// Create session
	meta := sessions.RequestMetadata(r)
	session, err := sessions.Create(ctx, s.db, provisioned.user.ID, orgMemberID, meta)
	if err != nil {
		logger.Error("failed to create session", "user_id", provisioned.user.ID, "error", err)
		s.errorRedirect(w, r, ErrSSOServerError)
		return
	}

	// Set session cookie
	cookie := sessions.CreateCookie(session.Token)
	http.SetCookie(w, cookie)

	// Redirect to the app
	baseURL := config.Get[string]("APP_BASE_URL")
	redirectURL := stateResult.RedirectURL
	if redirectURL == "" {
		redirectURL = baseURL
	}
	http.Redirect(w, r, redirectURL, http.StatusFound)
}

func (s *SSOMux) provision(
	ctx context.Context,
	provider enums.AuthProvider,
	userInfo *sso.UserInfo,
) (*provisionedSSOUser, error) {
	if userInfo.Organization != nil {
		return s.provisionOrganizationUser(ctx, provider, userInfo)
	}

	user, err := users.GetByAuthProvider(ctx, s.db, provider, userInfo.ProviderID)
	if err != nil {
		return nil, err
	}
	if user != nil {
		member, err := organizations.GetDefaultMemberForUser(ctx, s.db, user.ID)
		if err != nil {
			return nil, err
		}
		return &provisionedSSOUser{user: user, member: member}, nil
	}

	if err := checkSSOEmailAvailable(ctx, s.db, userInfo.Email); err != nil {
		return nil, err
	}

	result, err := users.RegisterWithAuthProviderAndPersonalOrg(
		ctx,
		s.db,
		optional.Set(randomUsername()),
		optionalEmail(userInfo.Email),
		provider,
		userInfo.ProviderID,
	)
	if err != nil {
		return nil, err
	}

	return &provisionedSSOUser{user: result.User, member: result.Member, created: true}, nil
}

func (s *SSOMux) provisionOrganizationUser(
	ctx context.Context,
	provider enums.AuthProvider,
	userInfo *sso.UserInfo,
) (*provisionedSSOUser, error) {
	organization := userInfo.Organization
	result := new(provisionedSSOUser)

	err := database.Transact(ctx, s.db, func(txn database.Database) error {
		user, err := users.GetByAuthProvider(ctx, txn, provider, userInfo.ProviderID)
		if err != nil {
			return err
		}
		if user == nil {
			if err := checkSSOEmailAvailable(ctx, txn, userInfo.Email); err != nil {
				return err
			}
			user, err = users.RegisterWithAuthProvider(
				ctx,
				txn,
				optional.Set(randomUsername()),
				optionalEmail(userInfo.Email),
				provider,
				userInfo.ProviderID,
			)
			if err != nil {
				return err
			}
			result.created = true
		}

		role := organizations.RoleMember
		if organization.Admin {
			role = organizations.RoleOwner
		}
		slug := fmt.Sprintf("%s-%s", strings.ToLower(organization.Slug), organization.ProviderID)
		_, member, err := orgidentities.Ensure(
			ctx,
			txn,
			user.ID,
			provider,
			organization.ProviderID,
			slug,
			organization.Name,
			role,
		)
		if err != nil {
			return err
		}

		result.user = user
		result.member = member
		return nil
	})
	if err != nil {
		return nil, err
	}

	return result, nil
}

func checkSSOEmailAvailable(ctx context.Context, db database.Database, email string) error {
	if email == "" {
		return nil
	}

	exists, err := users.EmailExists(ctx, db, email)
	if err != nil {
		return err
	}
	if exists {
		return errSSOEmailExists
	}
	return nil
}

func optionalEmail(email string) optional.Optional[string] {
	if email == "" {
		return optional.Empty[string]()
	}
	return optional.Set(email)
}

func (s *SSOMux) errorRedirect(w http.ResponseWriter, r *http.Request, err errors.ErrorDetail) {
	baseURL := config.Get[string]("APP_BASE_URL")
	redirectURL := fmt.Sprintf("%s/login?error=%s&message=%s",
		baseURL,
		err.Code,
		url.QueryEscape(err.Message))
	http.Redirect(w, r, redirectURL, http.StatusFound)
}

func providerToAuthProvider(provider string) enums.AuthProvider {
	switch provider {
	case "google":
		return enums.AuthProviderGoogle
	case "microsoft":
		return enums.AuthProviderMicrosoft
	case "github":
		return enums.AuthProviderGitHub
	case "apple":
		return enums.AuthProviderApple
	default:
		return enums.AuthProviderLocal
	}
}
