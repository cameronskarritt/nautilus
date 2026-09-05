package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"nautilus/internal/database/sessions"
	"nautilus/internal/database/users"
	"nautilus/internal/enums"
	"nautilus/internal/errors"
	"nautilus/internal/mux"
	"nautilus/internal/sso"
	"nautilus/internal/testutil"
	"nautilus/internal/testutil/require"
)

type testSSOProvider struct {
	code string
	err  error
}

func (p *testSSOProvider) Name() string { return "google" }

func (p *testSSOProvider) AuthURL(state string) string {
	return "https://accounts.example.test/authorize?state=" + url.QueryEscape(state)
}

func (p *testSSOProvider) Exchange(_ context.Context, code string) (*sso.UserInfo, error) {
	p.code = code
	return &sso.UserInfo{ProviderID: "google-user"}, p.err
}

func ssoRouter(s *SSOMux) *mux.Router {
	router := mux.New()
	s.Mount(router, "/sso")
	return router
}

func requireSSOError(t *testing.T, rec *httptest.ResponseRecorder, origin string, detail errors.ErrorDetail) {
	t.Helper()
	require.Equal(t, http.StatusFound, rec.Code)
	location, err := url.Parse(rec.Header().Get("Location"))
	require.NoError(t, err)
	require.Equal(t, origin, location.Scheme+"://"+location.Host)
	require.Equal(t, "/login", location.Path)
	require.Equal(t, string(detail.Code), location.Query().Get("error"))
	require.Equal(t, detail.Message, location.Query().Get("message"))
	for _, cookie := range rec.Result().Cookies() {
		require.NotEqual(t, sessions.CreateCookie("").Name, cookie.Name)
	}
}

func TestSSOMuxStart(t *testing.T) {
	tests := []struct {
		name     string
		redirect string
		want     string
	}{
		{name: "default frontend", want: "https://app.example.test"},
		{name: "user frontend", redirect: "https://app.example.test/dashboard?tab=profile", want: "https://app.example.test/dashboard?tab=profile"},
		{name: "admin frontend", redirect: "https://admin.example.test/dashboard", want: "https://admin.example.test/dashboard"},
		{name: "untrusted frontend", redirect: "https://evil.example.test/dashboard"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setSSOConfig(t, "https://admin.example.test")
			registry := sso.NewRegistry()
			registry.Register(new(testSSOProvider))
			router := ssoRouter(&SSOMux{registry: registry})
			req := httptest.NewRequest(http.MethodGet, "/sso/google?redirect="+url.QueryEscape(tt.redirect), nil)
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)

			if tt.want == "" {
				requireSSOError(t, rec, "https://app.example.test", ErrSSOInvalidRedirect)
				require.Empty(t, rec.Result().Cookies())
				return
			}
			require.Equal(t, http.StatusFound, rec.Code)
			location, err := url.Parse(rec.Header().Get("Location"))
			require.NoError(t, err)
			require.Equal(t, "accounts.example.test", location.Host)
			cookies := rec.Result().Cookies()
			require.Len(t, cookies, 1)
			req.AddCookie(cookies[0])
			state, err := sso.VerifyState(req, "test-sso-secret", location.Query().Get("state"))
			require.NoError(t, err)
			require.Equal(t, "google", state.Provider)
			require.Equal(t, tt.want, state.RedirectURL)
		})
	}
}

func TestSSOMuxCallbackErrors(t *testing.T) {
	tests := []struct {
		name          string
		redirect      string
		provider      string
		query         url.Values
		exchangeErr   error
		tampered      bool
		missingCookie bool
		removedAdmin  bool
		post          bool
		wantOrigin    string
		wantError     errors.ErrorDetail
		wantCode      string
	}{
		{name: "admin canceled", query: url.Values{"error": {"access_denied"}}, wantOrigin: "https://admin.example.test", wantError: ErrSSOProviderError},
		{name: "admin missing code", wantOrigin: "https://admin.example.test", wantError: ErrSSOMissingCode},
		{name: "admin exchange failed", query: url.Values{"code": {"bad-code"}}, exchangeErr: errors.New("provider failure"), wantOrigin: "https://admin.example.test", wantError: ErrSSOExchangeFailed, wantCode: "bad-code"},
		{name: "admin organization rejected", query: url.Values{"code": {"bad-code"}}, exchangeErr: sso.ErrOrganizationMembership, wantOrigin: "https://admin.example.test", wantError: ErrSSOOrganizationMembership, wantCode: "bad-code"},
		{name: "post provider error", post: true, query: url.Values{"error": {"access_denied"}}, wantOrigin: "https://admin.example.test", wantError: ErrSSOProviderError},
		{name: "user canceled", redirect: "https://app.example.test/dashboard", query: url.Values{"error": {"access_denied"}}, wantOrigin: "https://app.example.test", wantError: ErrSSOProviderError},
		{name: "tampered canceled state", tampered: true, query: url.Values{"error": {"access_denied"}}, wantOrigin: "https://app.example.test", wantError: ErrSSOInvalidState},
		{name: "missing state cookie", missingCookie: true, query: url.Values{"code": {"good-code"}}, wantOrigin: "https://app.example.test", wantError: ErrSSOInvalidState},
		{name: "provider mismatch", provider: "microsoft", query: url.Values{"code": {"good-code"}}, wantOrigin: "https://app.example.test", wantError: ErrSSOProviderMismatch},
		{name: "old untrusted redirect", redirect: "https://evil.example.test/dashboard", query: url.Values{"code": {"good-code"}}, wantOrigin: "https://app.example.test", wantError: ErrSSOInvalidRedirect},
		{name: "removed admin origin", removedAdmin: true, query: url.Values{"code": {"good-code"}}, wantOrigin: "https://app.example.test", wantError: ErrSSOInvalidRedirect},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			adminURL := "https://admin.example.test"
			if tt.removedAdmin {
				adminURL = ""
			}
			setSSOConfig(t, adminURL)
			provider := &testSSOProvider{err: tt.exchangeErr}
			registry := sso.NewRegistry()
			registry.Register(provider)
			router := ssoRouter(&SSOMux{registry: registry})
			redirect := tt.redirect
			if redirect == "" {
				redirect = "https://admin.example.test/dashboard?tab=users"
			}
			providerName := tt.provider
			if providerName == "" {
				providerName = "google"
			}
			stateRec := httptest.NewRecorder()
			state, err := sso.GenerateState(stateRec, "test-sso-secret", providerName, redirect)
			require.NoError(t, err)
			cookie := stateRec.Result().Cookies()[0]
			if tt.tampered {
				state += "tampered"
				cookie.Value = state
			}
			query := tt.query
			if query == nil {
				query = make(url.Values)
			}
			query.Set("state", state)
			req := httptest.NewRequest(http.MethodGet, "/sso/google/callback?"+query.Encode(), nil)
			if tt.post {
				req = httptest.NewRequest(http.MethodPost, "/sso/google/callback", strings.NewReader(query.Encode()))
				req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			}
			if !tt.missingCookie {
				req.AddCookie(cookie)
			}
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)

			requireSSOError(t, rec, tt.wantOrigin, tt.wantError)
			require.Equal(t, tt.wantCode, provider.code)
			cookies := rec.Result().Cookies()
			require.Len(t, cookies, 1)
			require.Equal(t, cookie.Name, cookies[0].Name)
			require.Equal(t, -1, cookies[0].MaxAge)
		})
	}
}

func TestSSOMuxCallbackSession(t *testing.T) {
	for _, target := range []string{
		"https://app.example.test/dashboard?tab=profile#details",
		"https://admin.example.test/dashboard?tab=users",
	} {
		t.Run(target, func(t *testing.T) {
			setSSOConfig(t, "https://admin.example.test")
			db := testutil.SetupTestDB(t)
			provider := new(testSSOProvider)
			registry := sso.NewRegistry()
			registry.Register(provider)
			router := ssoRouter(&SSOMux{db: db, registry: registry})
			stateRec := httptest.NewRecorder()
			state, err := sso.GenerateState(stateRec, "test-sso-secret", "google", target)
			require.NoError(t, err)
			req := httptest.NewRequest(http.MethodGet, "/sso/google/callback?code=good-code&state="+url.QueryEscape(state), nil)
			req.AddCookie(stateRec.Result().Cookies()[0])
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)

			require.Equal(t, http.StatusFound, rec.Code)
			require.Equal(t, target, rec.Header().Get("Location"))
			require.Equal(t, "good-code", provider.code)
			cookies := rec.Result().Cookies()
			require.Len(t, cookies, 2)
			require.Equal(t, -1, cookies[0].MaxAge)
			require.Equal(t, sessions.CreateCookie("").Name, cookies[1].Name)
			require.True(t, cookies[1].HttpOnly)
			require.True(t, cookies[1].Secure)
			session, err := sessions.Get(t.Context(), db, cookies[1].Value)
			require.NoError(t, err)
			require.NotNil(t, session)
			user, err := users.GetByAuthProvider(t.Context(), db, enums.AuthProviderGoogle, "google-user")
			require.NoError(t, err)
			require.NotNil(t, user)
			require.Equal(t, user.ID, session.UserID)
			require.True(t, session.OrgMemberID.Set)
		})
	}
}
